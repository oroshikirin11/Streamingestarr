package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
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
	// Whether the room has its own ingest passphrase; the value itself
	// never leaves the server.
	PassphraseSet bool `json:"passphraseSet"`
}

func roomToResponse(ch models.Channel) roomResponse {
	out := roomResponse{
		ID:             ch.ID,
		Name:           ch.Name,
		Keys:           ch.Keys,
		IsDefault:      ch.ID == channelrepository.DefaultChannelID,
		Title:          ch.Title,
		WelcomeMessage: ch.WelcomeMessage,
		LatencyLevel:   ch.LatencyLevel,
		SegmentFormat:  ch.SegmentFormat,
		OutputVariants: ch.OutputVariants,
		PassphraseSet:  ch.Passphrase != "",
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
	}
	return out
}

// GetRooms lists every room with its live state and stream keys.
func GetRooms(w http.ResponseWriter, r *http.Request) {
	rooms := []roomResponse{}
	for _, c := range channelrepository.ListChannels() {
		rooms = append(rooms, roomToResponse(c))
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
	_ = json.NewEncoder(w).Encode(roomToResponse(*channel))
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

// SetRoomPassphrase gives a room its own ingest passphrase, or clears it
// with "". Streams that open the room with one of its keys must then
// present it — on TCP as the preamble's second word, on SRT as the
// encryption passphrase — instead of the global one.
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
		// SRT's own rule, applied to both transports so one passphrase can
		// serve either; the TCP preamble is one line of words, so no
		// whitespace inside it.
		if len(pass) < 10 || len(pass) > 79 || strings.ContainsAny(pass, " \t\r\n") {
			webutils.WriteSimpleResponse(w, false, "a passphrase is 10 to 79 characters with no spaces")
			return
		}
	}
	if err := channelrepository.SetChannelPassphrase(req.ID, pass); err != nil {
		webutils.WriteSimpleResponse(w, false, err.Error())
		return
	}
	if pass == "" {
		webutils.WriteSimpleResponse(w, true, "room passphrase cleared — the global one applies")
		return
	}
	webutils.WriteSimpleResponse(w, true, "room passphrase set")
}
