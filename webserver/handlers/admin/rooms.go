package admin

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"streamingestarr/core"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/utils"
	webutils "streamingestarr/webserver/utils"
)

// The rooms surface: up to channelrepository.MaxChannels theaters, each a
// data row with its own stream key, created and deleted live from the admin
// page — no restart, no extra ports.

type roomResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	StreamKey   string `json:"streamKey,omitempty"`
	Online      bool   `json:"online"`
	ViewerCount int    `json:"viewerCount"`
	IsDefault   bool   `json:"isDefault"`
}

func roomToResponse(id, name, key string) roomResponse {
	out := roomResponse{
		ID:        id,
		Name:      name,
		StreamKey: key,
		IsDefault: id == channelrepository.DefaultChannelID,
	}
	if c := core.GetChannelRuntime(id); c != nil {
		status := c.GetStatus()
		out.Online = status.Online
		out.ViewerCount = status.ViewerCount
	}
	return out
}

// GetRooms lists every room with its live state and stream key.
func GetRooms(w http.ResponseWriter, r *http.Request) {
	rooms := []roomResponse{}
	for _, c := range channelrepository.ListChannels() {
		rooms = append(rooms, roomToResponse(c.ID, c.Name, c.StreamKey))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"rooms": rooms,
		"max":   channelrepository.MaxChannels,
	})
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
	for i := 2; i < 10; i++ {
		candidate := slug + "-" + string(rune('0'+i))
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
	_ = json.NewEncoder(w).Encode(roomToResponse(channel.ID, channel.Name, channel.StreamKey))
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

// RegenerateRoomKey mints a fresh stream key for a room: {"id": "..."}.
// A broadcast already running keeps going — keys are checked at connect.
func RegenerateRoomKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		webutils.WriteSimpleResponse(w, false, "send {\"id\": \"...\"}")
		return
	}
	key, err := utils.GenerateAccessToken()
	if err != nil {
		webutils.WriteSimpleResponse(w, false, "unable to generate a key")
		return
	}
	if err := channelrepository.SetChannelKey(req.ID, key); err != nil {
		webutils.WriteSimpleResponse(w, false, err.Error())
		return
	}
	channel := channelrepository.GetChannel(req.ID)
	if channel == nil {
		webutils.WriteSimpleResponse(w, false, "no such room")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(roomToResponse(channel.ID, channel.Name, channel.StreamKey))
}
