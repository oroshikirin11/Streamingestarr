package core

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"

	"streamingestarr/auth"
	"streamingestarr/core/chat"
	"streamingestarr/core/chat/events"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/persistence/configrepository"
)

// Room modes and access. A room is a theater, a relay, or both; its own
// password can guard the theater, the relay links, either or neither.
// Every surface that lets a viewer in — the HLS files, the chat socket,
// the status that carries the links — asks here.

// isAdmin says whether the request holds an admin session.
func isAdmin(r *http.Request) bool {
	return auth.RequestRole(r) == auth.RoleAdmin
}

// TheaterAllowed reports whether this request may watch and chat in the
// room: the room has a theater, and its lock (if any) is open for the
// session. Admins always pass.
func TheaterAllowed(r *http.Request, channelID string) bool {
	ch := channelrepository.GetChannel(channelID)
	if ch == nil {
		return false
	}
	if !ch.HasTheater() {
		return false
	}
	if !ch.LockTheater || isAdmin(r) {
		return true
	}
	return auth.RoomUnlocked(auth.TokenFromRequest(r), channelID)
}

// RelayLinksAllowed reports whether this request may read the room's
// relay links.
func RelayLinksAllowed(r *http.Request, channelID string) bool {
	ch := channelrepository.GetChannel(channelID)
	if ch == nil || !ch.Relays() {
		return false
	}
	if !ch.LockRelay || isAdmin(r) {
		return true
	}
	return auth.RoomUnlocked(auth.TokenFromRequest(r), channelID)
}

// TheaterLocked says whether the room asks THIS request for its password
// before the theater — false once the session has entered it.
func TheaterLocked(r *http.Request, ch *models.Channel) bool {
	if ch == nil || !ch.LockTheater || isAdmin(r) {
		return false
	}
	return !auth.RoomUnlocked(auth.TokenFromRequest(r), ch.ID)
}

// RelayLocked says whether the room asks THIS request for its password
// before the relay links.
func RelayLocked(r *http.Request, ch *models.Channel) bool {
	if ch == nil || !ch.LockRelay || isAdmin(r) {
		return false
	}
	return !auth.RoomUnlocked(auth.TokenFromRequest(r), ch.ID)
}

// UnlockRoom checks a viewer's attempt at the room password and, when it
// matches, remembers the session as unlocked. The error is the sentence
// for the viewer.
func UnlockRoom(r *http.Request, channelID, password string) error {
	ch := channelrepository.GetChannel(channelID)
	if ch == nil {
		return fmt.Errorf("no such room")
	}
	if ch.RoomPasswordHash == "" {
		return nil
	}
	if !auth.VerifyPassword(password, ch.RoomPasswordHash) {
		return fmt.Errorf("That is not this room's password.")
	}
	auth.MarkRoomUnlocked(auth.TokenFromRequest(r), channelID)
	return nil
}

// RelayLinks builds the room's links as the viewer's browser reached the
// server: the request's host and scheme, the RTSP port from config.
// Only the enabled kinds are present, in canonical order.
func RelayLinks(r *http.Request, ch *models.Channel) map[string]string {
	if ch == nil || !ch.Relays() || ch.RelayToken == "" {
		return map[string]string{}
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	if strings.Contains(hostname, ":") && !strings.HasPrefix(hostname, "[") {
		hostname = "[" + hostname + "]" // bare IPv6
	}
	links := map[string]string{}
	for _, p := range ch.EffectiveRelayProtocols() {
		switch p {
		case models.RelayProtocolRTSP:
			// rtspt: RTSP over TCP — the form AVPro (VRChat) expects;
			// UDP media through NAT is what breaks otherwise.
			links[p] = fmt.Sprintf("rtspt://%s:%d/%s/%s", hostname, configrepository.Get().GetRelayRTSPPort(), ch.ID, ch.RelayToken)
		case models.RelayProtocolTS:
			links[p] = fmt.Sprintf("%s://%s/relay/%s/%s.ts", scheme, host, ch.ID, ch.RelayToken)
		case models.RelayProtocolHLS:
			links[p] = fmt.Sprintf("%s://%s/relay/%s/%s.m3u8", scheme, host, ch.ID, ch.RelayToken)
		}
	}
	return links
}

// SetRoomMode stores a room's mode and link kinds, then tells the room:
// a system line and a ROOM_MODE event, so open pages switch in place.
func SetRoomMode(channelID, mode string, protocols []string) error {
	ch := channelrepository.GetChannel(channelID)
	if ch == nil {
		return fmt.Errorf("no such room")
	}
	before := ch.EffectiveMode()
	if err := channelrepository.SetChannelMode(channelID, mode, protocols); err != nil {
		return err
	}
	if before != mode {
		line := ""
		switch mode {
		case models.RoomModeRelay:
			line = "The room switched to relay mode"
		case models.RoomModeBoth:
			line = "The room now relays alongside the theater"
		default:
			line = "The room is a theater again"
		}
		if before == models.RoomModeRelay {
			line = "The room is a theater again"
		}
		_ = chat.SendSystemAction(channelID, line, true)
		log.Infof("room %s: mode %s → %s", channelID, before, mode)
	}
	chat.SendPayloadToChannel(channelID, events.EventPayload{"type": events.RoomModeChanged, "mode": mode})
	if c := GetChannelRuntime(channelID); c != nil {
		// A relay-only room has no seats, so no votes.
		c.RecomputePauseVote()
	}
	return nil
}

// chatRoomAccess is the hook chat asks before seating a socket.
func chatRoomAccess(r *http.Request, channelID string) bool {
	if channelID == "" {
		channelID = channelrepository.DefaultChannelID
	}
	return TheaterAllowed(r, channelID)
}
