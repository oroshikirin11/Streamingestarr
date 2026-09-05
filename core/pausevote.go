package core

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"streamingestarr/core/chat"
	"streamingestarr/core/chat/events"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
)

// Viewer pause votes. A room's viewers can vote the broadcast paused and,
// once paused, resumed; at half the seated viewers the receiver tells the
// sender over the control channel (control.go). The arithmetic lives in
// voteBoard, pure and testable; pauseVoteRoom wraps it with the chat
// messages, the sender connection and the timers.

const (
	// pauseVoteTTL is how long a vote stands before it must be recast.
	pauseVoteTTL = 90 * time.Second
	// pauseVoteCooldown follows every state change: votes are accepted
	// but not evaluated, so a fresh pause is not undone by momentum.
	pauseVoteCooldown = 30 * time.Second
	// pauseVotePendingTimeout is how long a command may wait for the
	// sender's answer before the room gives up on it.
	pauseVotePendingTimeout = 15 * time.Second

	voteActionPause  = "pause"
	voteActionResume = "resume"
)

type voteEntry struct {
	Action string
	Name   string
	At     time.Time
}

// voteBoard is the vote arithmetic of one room: who voted for what, what
// the broadcast is doing, and whether a command is already on its way.
// No locks, no network, no clock of its own — every method takes now.
type voteBoard struct {
	votes         map[string]voteEntry
	paused        bool
	pending       string
	cooldownUntil time.Time
}

// Wanted is the only action that counts right now: pause while playing,
// resume while paused.
func (b *voteBoard) Wanted() string {
	if b.paused {
		return voteActionResume
	}
	return voteActionPause
}

// Cast records a user's vote. It reports whether the tally changed: a
// vote for the wrong phase is ignored, and recasting the same vote only
// refreshes its clock.
func (b *voteBoard) Cast(userID, name, action string, now time.Time) bool {
	if action != b.Wanted() {
		return false
	}
	if b.votes == nil {
		b.votes = map[string]voteEntry{}
	}
	previous, had := b.votes[userID]
	b.votes[userID] = voteEntry{Action: action, Name: name, At: now}
	return !had || previous.Action != action || now.Sub(previous.At) > pauseVoteTTL
}

// Withdraw removes a user's vote, reporting whether there was one.
func (b *voteBoard) Withdraw(userID string) bool {
	if _, had := b.votes[userID]; !had {
		return false
	}
	delete(b.votes, userID)
	return true
}

// Expire drops votes older than the TTL.
func (b *voteBoard) Expire(now time.Time) bool {
	changed := false
	for id, v := range b.votes {
		if now.Sub(v.At) > pauseVoteTTL {
			delete(b.votes, id)
			changed = true
		}
	}
	return changed
}

// Retain drops the votes of users who are no longer seated.
func (b *voteBoard) Retain(seated func(userID string) bool) bool {
	changed := false
	for id := range b.votes {
		if !seated(id) {
			delete(b.votes, id)
			changed = true
		}
	}
	return changed
}

// Voters lists the standing votes for the wanted action: ids and names.
func (b *voteBoard) Voters(now time.Time) (ids []string, names []string) {
	ids, names = []string{}, []string{}
	wanted := b.Wanted()
	for id, v := range b.votes {
		if v.Action == wanted && now.Sub(v.At) <= pauseVoteTTL {
			ids = append(ids, id)
			names = append(names, v.Name)
		}
	}
	return ids, names
}

// Count is how many standing votes the wanted action has.
func (b *voteBoard) Count(now time.Time) int {
	ids, _ := b.Voters(now)
	return len(ids)
}

// voteThreshold is half the seated viewers, rounded up, never below one.
func voteThreshold(viewers int) int {
	if viewers < 1 {
		return 1
	}
	return int(math.Ceil(float64(viewers) / 2))
}

// InCooldown reports whether a state change is too fresh to act on.
func (b *voteBoard) InCooldown(now time.Time) bool {
	return now.Before(b.cooldownUntil)
}

// Ready reports whether the wanted action should be sent: threshold met,
// nothing pending, cooldown over.
func (b *voteBoard) Ready(viewers int, now time.Time) bool {
	if b.pending != "" || b.InCooldown(now) {
		return false
	}
	return b.Count(now) >= voteThreshold(viewers)
}

// StartPending marks a command as sent: the tally is spent.
func (b *voteBoard) StartPending(action string) {
	b.pending = action
	b.votes = map[string]voteEntry{}
}

// ClearPending forgets a command the sender refused or never answered.
func (b *voteBoard) ClearPending() {
	b.pending = ""
}

// SetPaused applies the sender's truth. A change clears the votes, ends
// any pending command, and starts the cooldown; it reports whether the
// state actually changed.
func (b *voteBoard) SetPaused(paused bool, now time.Time) bool {
	if b.paused == paused {
		return false
	}
	b.paused = paused
	b.pending = ""
	b.votes = map[string]voteEntry{}
	b.cooldownUntil = now.Add(pauseVoteCooldown)
	return true
}

// Reset forgets everything — the broadcast ended.
func (b *voteBoard) Reset() {
	b.votes = map[string]voteEntry{}
	b.paused = false
	b.pending = ""
	b.cooldownUntil = time.Time{}
}

// pauseVoteRoom is the live vote state of one room.
type pauseVoteRoom struct {
	mu    sync.Mutex
	board voteBoard
	// control is the sender's connection, nil when none is up.
	control *controlConn
	// pendingID is the id of the command awaiting the sender's answer.
	pendingID    string
	pendingSince time.Time
	// refuseReason is the sender's reason for the last refusal, shown in
	// the tray until the next vote or state change.
	refuseReason string
	// lastState is the last PAUSE_VOTE_STATE broadcast, so an unchanged
	// tally is never re-sent.
	lastState string
	// lastLine is the last "Paused/Resumed by viewer vote" line, keyed by
	// state, so the same state never announces twice.
	lastLine string
	timer    *time.Timer
}

// HandlePauseVote applies a viewer's PAUSE_VOTE message.
func (c *ChannelRuntime) HandlePauseVote(user *models.User, action string) {
	if user == nil {
		return
	}
	pv := &c.pauseVote
	now := time.Now()
	viewers := chat.DistinctUserCount(c.ID)

	pv.mu.Lock()
	available, _ := c.pauseVoteAvailability()
	line := ""
	switch action {
	case "withdraw":
		if pv.board.Withdraw(user.ID) {
			line = fmt.Sprintf("%s withdrew their vote (%d of %d)", user.DisplayName, pv.board.Count(now), viewers)
		}
	case voteActionPause, voteActionResume:
		if available && pv.board.Cast(user.ID, user.DisplayName, action, now) {
			pv.refuseReason = ""
			line = fmt.Sprintf("%s voted to %s (%d of %d)", user.DisplayName, action, pv.board.Count(now), viewers)
		}
	}
	pv.mu.Unlock()

	if line != "" {
		_ = chat.SendSystemAction(c.ID, line, true)
	}
	c.recomputePauseVote(0)
}

// pauseVoteAvailability says whether votes are accepted right now and,
// when not, why. Caller holds pv.mu.
func (c *ChannelRuntime) pauseVoteAvailability() (bool, string) {
	np := c.GetNowPlaying()
	if np == nil || np.Controls == nil || !np.Controls.PauseVote {
		return false, "the sender does not allow pause votes"
	}
	if ch := channelrepository.GetChannel(c.ID); ch != nil && !ch.PauseVoteEnabled {
		return false, "pause votes are switched off for this room"
	}
	if c.pauseVote.control == nil {
		return false, "the sender is not connected"
	}
	if np.Paused && np.PausedBy == "host" {
		return false, "the host paused the stream"
	}
	return true, ""
}

// PauseVoteInfo reports the room's switch and whether a sender control
// connection is up — for the status endpoints.
func (c *ChannelRuntime) PauseVoteInfo() (enabled bool, controlConnected bool) {
	enabled = true
	if ch := channelrepository.GetChannel(c.ID); ch != nil {
		enabled = ch.PauseVoteEnabled
	}
	c.pauseVote.mu.Lock()
	controlConnected = c.pauseVote.control != nil
	c.pauseVote.mu.Unlock()
	return enabled, controlConnected
}

// RecomputePauseVote re-evaluates the room's tally: after the admin
// switch flips, a viewer joins or leaves, a vote lands or expires, the
// sender connects, answers or changes state.
func (c *ChannelRuntime) RecomputePauseVote() {
	c.recomputePauseVote(0)
}

// recomputePauseVote prunes, evaluates, fires the command when due, and
// broadcasts the state when it changed. newClientID, when not 0, is a
// connection that needs the state even when nothing changed.
func (c *ChannelRuntime) recomputePauseVote(newClientID uint) {
	pv := &c.pauseVote
	now := time.Now()
	viewers := chat.DistinctUserCount(c.ID)

	pv.mu.Lock()
	pv.board.Expire(now)
	pv.board.Retain(func(userID string) bool { return chat.UserSeated(c.ID, userID) })

	// A command the sender never answered must not block the room.
	if pv.board.pending != "" && pv.pendingID != "" && now.Sub(pv.pendingSince) > pauseVotePendingTimeout {
		pv.board.ClearPending()
		pv.pendingID = ""
		pv.refuseReason = "the sender did not answer"
	}

	available, reason := c.pauseVoteAvailability()
	refusedLine := ""
	if available && pv.board.Ready(viewers, now) {
		action := pv.board.Wanted()
		ids, names := pv.board.Voters(now)
		cmd := controlCommand{Type: action, Votes: len(ids), Viewers: viewers, By: names, ID: newCommandID()}
		if err := pv.control.send(cmd); err != nil {
			log.Debugln("pause vote: unable to reach the sender:", err)
			pv.refuseReason = "the sender is not reachable"
			refusedLine = fmt.Sprintf("The stream cannot %s right now — the sender is not reachable", action)
		} else {
			pv.board.StartPending(action)
			pv.pendingID = cmd.ID
			pv.pendingSince = now
			pv.refuseReason = ""
		}
	}

	payload := c.pauseVoteStatePayload(viewers, available, reason, now)
	data, _ := json.Marshal(payload)
	changed := string(data) != pv.lastState
	pv.lastState = string(data)
	c.schedulePauseVoteTimer(now)
	pv.mu.Unlock()

	if refusedLine != "" {
		_ = chat.SendSystemAction(c.ID, refusedLine, true)
	}
	if changed {
		chat.SendPayloadToChannel(c.ID, payload)
	} else if newClientID != 0 {
		chat.SendPayloadToClient(newClientID, payload)
	}
}

// pauseVoteStatePayload is the PAUSE_VOTE_STATE frame. Caller holds pv.mu.
func (c *ChannelRuntime) pauseVoteStatePayload(viewers int, available bool, reason string, now time.Time) events.EventPayload {
	pv := &c.pauseVote
	np := c.GetNowPlaying()
	paused, pausedBy, pending := false, "", pv.board.pending
	var pausedAt int64
	if np != nil {
		paused, pausedBy, pausedAt = np.Paused, np.PausedBy, np.PausedAt
		if pending == "" {
			pending = np.Pending
		}
	}
	if available && pv.refuseReason != "" {
		reason = pv.refuseReason
	}
	var cooldownUntil int64
	if pv.board.InCooldown(now) {
		cooldownUntil = pv.board.cooldownUntil.Unix()
	}
	ids, _ := pv.board.Voters(now)
	return events.EventPayload{
		"type":          events.PauseVoteState,
		"action":        pv.board.Wanted(),
		"votes":         len(ids),
		"needed":        voteThreshold(viewers),
		"viewers":       viewers,
		"pending":       pending,
		"paused":        paused,
		"pausedBy":      pausedBy,
		"pausedAt":      pausedAt,
		"cooldownUntil": cooldownUntil,
		"available":     available,
		"reason":        reason,
		// voters is an extension for the theater: the ids whose vote
		// stands, so a viewer's own button renders pressed after a reload.
		"voters": ids,
	}
}

// schedulePauseVoteTimer arms one timer for the next moment the tally
// changes on its own: a vote expiring, the cooldown ending, a pending
// command timing out. Caller holds pv.mu.
func (c *ChannelRuntime) schedulePauseVoteTimer(now time.Time) {
	pv := &c.pauseVote
	var next time.Time
	consider := func(t time.Time) {
		if t.After(now) && (next.IsZero() || t.Before(next)) {
			next = t
		}
	}
	for _, v := range pv.board.votes {
		consider(v.At.Add(pauseVoteTTL))
	}
	consider(pv.board.cooldownUntil)
	if pv.board.pending != "" && pv.pendingID != "" {
		consider(pv.pendingSince.Add(pauseVotePendingTimeout))
	}
	if pv.timer != nil {
		pv.timer.Stop()
		pv.timer = nil
	}
	if !next.IsZero() {
		pv.timer = time.AfterFunc(next.Sub(now)+50*time.Millisecond, func() { c.recomputePauseVote(0) })
	}
}

// applyPauseTransition reacts to the sender's paused flag changing, from
// a push or a control state frame: clears the tally, starts the cooldown
// and announces a pause or resume that the viewers voted for.
func (c *ChannelRuntime) applyPauseTransition() {
	np := c.GetNowPlaying()
	pv := &c.pauseVote
	now := time.Now()

	pv.mu.Lock()
	paused := np != nil && np.Paused
	// What the room had asked for, before the change spends it: a state
	// that answers our own command is the viewers' doing even when the
	// sender reports pausedBy "" once playing again.
	asked := pv.board.pending
	changed := pv.board.SetPaused(paused, now)
	if changed {
		pv.pendingID = ""
		pv.refuseReason = ""
	}
	line, key := "", ""
	byViewers := np != nil && np.PausedBy == "viewers"
	if changed && paused && (byViewers || asked == voteActionPause) {
		line, key = "Paused by viewer vote", "paused"
	}
	if changed && !paused && (byViewers || asked == voteActionResume) {
		line, key = "Resumed by viewer vote", "resumed"
	}
	if key != "" && key == pv.lastLine {
		line = ""
	}
	if key != "" {
		pv.lastLine = key
	}
	pv.mu.Unlock()

	if line != "" {
		_ = chat.SendSystemAction(c.ID, line, true)
	}
	c.recomputePauseVote(0)
}

// resetPauseVote forgets the room's votes — the broadcast ended.
func (c *ChannelRuntime) resetPauseVote() {
	pv := &c.pauseVote
	pv.mu.Lock()
	pv.board.Reset()
	pv.pendingID = ""
	pv.refuseReason = ""
	pv.lastLine = ""
	pv.mu.Unlock()
	c.recomputePauseVote(0)
}

// handleControlAck applies the sender's answer to a command.
func (c *ChannelRuntime) handleControlAck(frame controlFrame) {
	pv := &c.pauseVote
	pv.mu.Lock()
	if frame.ID == "" || frame.ID != pv.pendingID {
		pv.mu.Unlock()
		return
	}
	line := ""
	if !frame.OK {
		action := pv.board.pending
		if action == "" {
			action = voteActionPause
		}
		reason := frame.Reason
		if reason == "" {
			reason = "the sender refused"
		}
		pv.board.ClearPending()
		pv.pendingID = ""
		pv.refuseReason = reason
		line = fmt.Sprintf("The stream cannot %s right now — %s", action, reason)
	}
	pv.mu.Unlock()

	if line != "" {
		_ = chat.SendSystemAction(c.ID, line, true)
	}
	c.recomputePauseVote(0)
}

// handleControlState applies a sender state frame to the room's
// now-playing — the last word wins, push or frame.
func (c *ChannelRuntime) handleControlState(frame controlFrame) {
	now := time.Now()

	_metadataLock.Lock()
	previous := c.nowPlaying
	var np models.NowPlaying
	if previous != nil {
		np = *previous
	} else {
		np.ReceivedAt = now
	}
	np.Paused = frame.Paused
	np.PausedBy = frame.PausedBy
	np.Pending = frame.Pending
	np.Controls = &models.NowPlayingControls{PauseVote: frame.PauseVote}
	if !frame.Paused {
		np.PausedAt = 0
	} else if np.PausedAt == 0 {
		np.PausedAt = now.Unix()
	}
	c.nowPlaying = &np
	_metadataLock.Unlock()

	c.applyPauseTransition()
}

// pauseVoteHook is what chat calls with a viewer's PAUSE_VOTE.
func pauseVoteHook(channelID string, user *models.User, action string) {
	if c := GetChannelRuntime(channelID); c != nil {
		c.HandlePauseVote(user, action)
	}
}

// presenceHook is what chat calls when a room's seating changes.
func presenceHook(channelID string, clientID uint) {
	if c := GetChannelRuntime(channelID); c != nil {
		c.recomputePauseVote(clientID)
	}
}
