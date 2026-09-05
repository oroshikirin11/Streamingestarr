package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"streamingestarr/auth"
	"strings"

	"streamingestarr/core"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/persistence/configrepository"
	webutils "streamingestarr/webserver/utils"
)

// The rooms surface: up to channelrepository.MaxChannels theaters, each a
// data row with its own stream key, created and deleted live from the admin
// page — no restart, no extra ports.

type roomResponse struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Keys        []models.ChannelKey `json:"keys"`
	Online      bool                `json:"online"`
	ViewerCount int                 `json:"viewerCount"`
	IsDefault   bool                `json:"isDefault"`

	// Per-room broadcast configuration; zero values inherit the server
	// defaults from the Video/Settings sections.
	Title          string                       `json:"title"`
	WelcomeMessage string                       `json:"welcomeMessage"`
	LatencyLevel   int                          `json:"latencyLevel"`
	SegmentFormat  string                       `json:"segmentFormat"`
	OutputVariants []models.StreamOutputVariant `json:"outputVariants"`
	// Whether the room has its own SRT passphrase; the value itself
	// never leaves the server.
	PassphraseSet bool `json:"passphraseSet"`
	// PauseVoteEnabled is the room's "viewers may vote to pause" switch.
	PauseVoteEnabled bool `json:"pauseVoteEnabled"`

	// Mode, the relay's link kinds and links (as reached through this
	// request's host), and the room's own password and what it guards.
	Mode            string            `json:"mode"`
	RelayProtocols  []string          `json:"relayProtocols"`
	RelayLinks      map[string]string `json:"relayLinks"`
	RelayPlayers    int               `json:"relayPlayers"`
	RelayEncoding   string            `json:"relayEncoding"`
	RoomPasswordSet bool              `json:"roomPasswordSet"`
	LockTheater     bool              `json:"lockTheater"`
	LockRelay       bool              `json:"lockRelay"`
}

func roomToResponse(ch models.Channel, r *http.Request) roomResponse {
	out := roomResponse{
		ID:               ch.ID,
		Name:             ch.Name,
		Keys:             ch.Keys,
		IsDefault:        ch.ID == channelrepository.DefaultChannelID,
		Title:            ch.Title,
		WelcomeMessage:   ch.WelcomeMessage,
		LatencyLevel:     ch.LatencyLevel,
		SegmentFormat:    ch.SegmentFormat,
		OutputVariants:   ch.OutputVariants,
		PassphraseSet:    ch.Passphrase != "",
		PauseVoteEnabled: ch.PauseVoteEnabled,
		Mode:             ch.EffectiveMode(),
		RelayProtocols:   ch.EffectiveRelayProtocols(),
		RelayLinks:       core.RelayLinks(r, &ch),
		RoomPasswordSet:  ch.RoomPasswordHash != "",
		LockTheater:      ch.LockTheater,
		LockRelay:        ch.LockRelay,
	}
	if out.OutputVariants == nil {
		out.OutputVariants = []models.StreamOutputVariant{}
	}
	if out.Keys == nil {
		out.Keys = []models.ChannelKey{}
	}
	if c := core.GetChannelRuntime(ch.ID); c != nil {
		status := c.GetStatus()
		out.Online = status.Online
		out.ViewerCount = status.ViewerCount
		out.RelayPlayers = c.RelayPlayerCount()
		out.RelayEncoding = c.RelayEncoding()
	}
	return out
}

// GetRooms lists every room with its live state and stream keys.
func GetRooms(w http.ResponseWriter, r *http.Request) {
	rooms := []roomResponse{}
	for _, c := range channelrepository.ListChannels() {
		rooms = append(rooms, roomToResponse(c, r))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"rooms": rooms})
}

var roomSlugStrip = regexp.MustCompile(`[^a-z0-9-]+`)

// roomIDFromName derives a URL/directory-safe unique id from a display name.
func roomIDFromName(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = roomSlugStrip.ReplaceAllString(strings.ReplaceAll(slug, " ", "-"), "")
	slug = strings.Trim(slug, "-")
	if len(slug) > 24 {
		slug = slug[:24]
	}
	if slug == "" || slug[0] < 'a' || slug[0] > 'z' {
		slug = "room" + slug
	}
	if channelrepository.GetChannel(slug) == nil && channelrepository.ValidChannelID.MatchString(slug) {
		return slug
	}
	for i := 2; i < 100; i++ {
		candidate := fmt.Sprintf("%s-%d", slug, i)
		if channelrepository.GetChannel(candidate) == nil && channelrepository.ValidChannelID.MatchString(candidate) {
			return candidate
		}
	}
	return ""
}

// CreateRoom creates a new room from {"name": "..."} and returns it with
// its freshly minted stream key.
func CreateRoom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		webutils.WriteSimpleResponse(w, false, "send {\"name\": \"...\"}")
		return
	}
	name := strings.TrimSpace(req.Name)
	if len(name) > 64 {
		webutils.WriteSimpleResponse(w, false, "the name is too long (max 64)")
		return
	}
	id := roomIDFromName(name)
	if id == "" {
		webutils.WriteSimpleResponse(w, false, "unable to derive a unique room id from that name")
		return
	}
	channel, err := core.AddChannel(id, name)
	if err != nil {
		webutils.WriteSimpleResponse(w, false, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(roomToResponse(*channel, r))
}

// DeleteRoom removes a room by {"id": "..."} — a live broadcast in it is
// disconnected first; the default room is protected.
func DeleteRoom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		webutils.WriteSimpleResponse(w, false, "send {\"id\": \"...\"}")
		return
	}
	if err := core.RemoveChannel(req.ID); err != nil {
		webutils.WriteSimpleResponse(w, false, err.Error())
		return
	}
	webutils.WriteSimpleResponse(w, true, "room deleted")
}

// RenameRoom updates a room's display name: {"id": "...", "name": "..."}.
func RenameRoom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.ID == "" || strings.TrimSpace(req.Name) == "" {
		webutils.WriteSimpleResponse(w, false, "send {\"id\": \"...\", \"name\": \"...\"}")
		return
	}
	if len(strings.TrimSpace(req.Name)) > 64 {
		webutils.WriteSimpleResponse(w, false, "the name is too long (max 64)")
		return
	}
	if channelrepository.GetChannel(req.ID) == nil {
		webutils.WriteSimpleResponse(w, false, "no such room")
		return
	}
	if err := channelrepository.SetChannelName(req.ID, strings.TrimSpace(req.Name)); err != nil {
		webutils.WriteSimpleResponse(w, false, err.Error())
		return
	}
	webutils.WriteSimpleResponse(w, true, "room renamed")
}

// SetRoomKeys replaces a room's key list — the same edit-then-save flow
// the global list uses: {"id": "...", "keys": [{"key": "...", "comment": ""}]}.
// A broadcast already running keeps going — keys are checked at connect.
func SetRoomKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string              `json:"id"`
		Keys []models.ChannelKey `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		webutils.WriteSimpleResponse(w, false, "send {\"id\": \"...\", \"keys\": [...]}")
		return
	}
	if channelrepository.GetChannel(req.ID) == nil {
		webutils.WriteSimpleResponse(w, false, "no such room")
		return
	}
	// A room key colliding with the global list would hijack a main-room
	// key (room keys win the routing lookup) — refuse the ambiguity.
	for _, global := range configrepository.Get().GetStreamKeys() {
		for _, k := range req.Keys {
			if global.Key != nil && *global.Key != "" && *global.Key == k.Key {
				webutils.WriteSimpleResponse(w, false, "the key "+k.Key+" already belongs to the main room")
				return
			}
		}
	}
	if err := channelrepository.ReplaceChannelKeys(req.ID, req.Keys); err != nil {
		webutils.WriteSimpleResponse(w, false, err.Error())
		return
	}
	webutils.WriteSimpleResponse(w, true, "keys saved")
}

// SetRoomConfig stores a room's broadcast configuration. The whole config
// is replaced at once (the UI sends its full state). Zero values — "" for
// text and format, -1 for latency, [] for variants — mean "inherit the
// server defaults". Takes effect on the room's NEXT broadcast.
func SetRoomConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID             string                       `json:"id"`
		Title          string                       `json:"title"`
		WelcomeMessage string                       `json:"welcomeMessage"`
		LatencyLevel   *int                         `json:"latencyLevel"`
		SegmentFormat  string                       `json:"segmentFormat"`
		OutputVariants []models.StreamOutputVariant `json:"outputVariants"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		webutils.WriteSimpleResponse(w, false, "send {\"id\": \"...\", ...config}")
		return
	}
	if channelrepository.GetChannel(req.ID) == nil {
		webutils.WriteSimpleResponse(w, false, "no such room")
		return
	}
	latency := -1
	if req.LatencyLevel != nil {
		latency = *req.LatencyLevel
	}
	cfg := models.Channel{
		Title:          strings.TrimSpace(req.Title),
		WelcomeMessage: strings.TrimSpace(req.WelcomeMessage),
		LatencyLevel:   latency,
		SegmentFormat:  req.SegmentFormat,
		OutputVariants: req.OutputVariants,
	}
	if err := channelrepository.SetChannelConfig(req.ID, cfg); err != nil {
		webutils.WriteSimpleResponse(w, false, err.Error())
		return
	}
	webutils.WriteSimpleResponse(w, true, "room configuration saved")
}

// SetRoomPassphrase gives a room its own SRT passphrase, or clears it
// with "". Streams that open the room over SRT with one of its keys must
// then use it as the encryption passphrase instead of the global SRT one.
// (The TCP ingest has no passphrase — TLS is its lock.)
func SetRoomPassphrase(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         string `json:"id"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		webutils.WriteSimpleResponse(w, false, "send {\"id\": \"...\", \"passphrase\": \"...\"}")
		return
	}
	if channelrepository.GetChannel(req.ID) == nil {
		webutils.WriteSimpleResponse(w, false, "no such room")
		return
	}
	pass := strings.TrimSpace(req.Passphrase)
	if pass != "" {
		// SRT's own rule: 10 to 79 characters. No whitespace either, so
		// it survives every sender's URL/config field intact.
		if len(pass) < 10 || len(pass) > 79 || strings.ContainsAny(pass, " \t\r\n") {
			webutils.WriteSimpleResponse(w, false, "an SRT passphrase is 10 to 79 characters with no spaces")
			return
		}
	}
	if err := channelrepository.SetChannelPassphrase(req.ID, pass); err != nil {
		webutils.WriteSimpleResponse(w, false, err.Error())
		return
	}
	if pass == "" {
		webutils.WriteSimpleResponse(w, true, "room SRT passphrase cleared — the global SRT passphrase applies")
		return
	}
	webutils.WriteSimpleResponse(w, true, "room SRT passphrase set")
}

// SetRoomPauseVote flips a room's "viewers may vote to pause" switch:
// {"id": "...", "enabled": true|false}. Takes effect at once — the room
// re-evaluates its tally and tells the viewers.
func SetRoomPauseVote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Enabled *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.Enabled == nil {
		webutils.WriteSimpleResponse(w, false, "send {\"id\": \"...\", \"enabled\": true|false}")
		return
	}
	if channelrepository.GetChannel(req.ID) == nil {
		webutils.WriteSimpleResponse(w, false, "no such room")
		return
	}
	if err := channelrepository.SetChannelPauseVote(req.ID, *req.Enabled); err != nil {
		webutils.WriteSimpleResponse(w, false, err.Error())
		return
	}
	if c := core.GetChannelRuntime(req.ID); c != nil {
		c.RecomputePauseVote()
	}
	if *req.Enabled {
		webutils.WriteSimpleResponse(w, true, "viewers may vote to pause")
		return
	}
	webutils.WriteSimpleResponse(w, true, "pause votes are off for this room")
}

// SetRoomMode sets what a room is and which relay links it offers:
// {"id": "...", "mode": "theater|relay|both", "relayProtocols": ["rtsp", "ts", "hls"]}.
func SetRoomMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID             string   `json:"id"`
		Mode           string   `json:"mode"`
		RelayProtocols []string `json:"relayProtocols"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		webutils.WriteSimpleResponse(w, false, "send {\"id\": \"...\", \"mode\": \"theater|relay|both\", \"relayProtocols\": [...]}")
		return
	}
	if channelrepository.GetChannel(req.ID) == nil {
		webutils.WriteSimpleResponse(w, false, "no such room")
		return
	}
	if err := core.SetRoomMode(req.ID, strings.ToLower(strings.TrimSpace(req.Mode)), req.RelayProtocols); err != nil {
		webutils.WriteSimpleResponse(w, false, err.Error())
		return
	}
	switch req.Mode {
	case models.RoomModeRelay:
		webutils.WriteSimpleResponse(w, true, "Relay mode — the theater is closed, the links are live")
	case models.RoomModeBoth:
		webutils.WriteSimpleResponse(w, true, "Theater and relay")
	default:
		webutils.WriteSimpleResponse(w, true, "Theater mode")
	}
}

// SetRoomPassword sets or clears ("") a room's own password. A change
// forgets every session that had entered the old one.
func SetRoomPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		webutils.WriteSimpleResponse(w, false, "send {\"id\": \"...\", \"password\": \"...\"}")
		return
	}
	if channelrepository.GetChannel(req.ID) == nil {
		webutils.WriteSimpleResponse(w, false, "no such room")
		return
	}
	req.Password = strings.TrimSpace(req.Password)
	if len(req.Password) > 128 {
		webutils.WriteSimpleResponse(w, false, "the room password can be up to 128 characters")
		return
	}
	hash := ""
	if req.Password != "" {
		h, err := auth.HashPassword(req.Password)
		if err != nil {
			webutils.WriteSimpleResponse(w, false, "unable to hash the password")
			return
		}
		hash = h
	}
	if err := channelrepository.SetChannelPassword(req.ID, hash); err != nil {
		webutils.WriteSimpleResponse(w, false, err.Error())
		return
	}
	auth.ClearRoomUnlocks(req.ID)
	if hash == "" {
		webutils.WriteSimpleResponse(w, true, "Room password removed — nothing is locked")
		return
	}
	webutils.WriteSimpleResponse(w, true, "Room password set — choose what it guards")
}

// SetRoomLocks says what the room password guards:
// {"id": "...", "lockTheater": bool, "lockRelay": bool}.
func SetRoomLocks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          string `json:"id"`
		LockTheater bool   `json:"lockTheater"`
		LockRelay   bool   `json:"lockRelay"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		webutils.WriteSimpleResponse(w, false, "send {\"id\": \"...\", \"lockTheater\": bool, \"lockRelay\": bool}")
		return
	}
	ch := channelrepository.GetChannel(req.ID)
	if ch == nil {
		webutils.WriteSimpleResponse(w, false, "no such room")
		return
	}
	if ch.RoomPasswordHash == "" && (req.LockTheater || req.LockRelay) {
		webutils.WriteSimpleResponse(w, false, "set a room password first")
		return
	}
	if err := channelrepository.SetChannelLocks(req.ID, req.LockTheater, req.LockRelay); err != nil {
		webutils.WriteSimpleResponse(w, false, err.Error())
		return
	}
	webutils.WriteSimpleResponse(w, true, "Locks saved")
}

// NewRelayToken rotates a room's relay token: every old link stops
// working and connected relay players are dropped. {"id": "..."}.
func NewRelayToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		webutils.WriteSimpleResponse(w, false, "send {\"id\": \"...\"}")
		return
	}
	if channelrepository.GetChannel(req.ID) == nil {
		webutils.WriteSimpleResponse(w, false, "no such room")
		return
	}
	if _, err := channelrepository.RotateRelayToken(req.ID); err != nil {
		webutils.WriteSimpleResponse(w, false, err.Error())
		return
	}
	if c := core.GetChannelRuntime(req.ID); c != nil {
		c.DropRelayPlayers()
	}
	webutils.WriteSimpleResponse(w, true, "New links — the old ones stopped working")
}
