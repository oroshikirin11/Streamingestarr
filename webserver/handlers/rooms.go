package handlers

import (
	"encoding/json"
	"net/http"

	"streamingestarr/core"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/persistence/configrepository"
	"streamingestarr/webserver/router/middleware"
)

// The viewer-facing rooms list: what the lobby renders after login — which
// theaters exist, which are playing, and what's on. No stream keys here.

type viewerRoom struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
	// Mode: theater, relay or both — the wall badges relays.
	Mode        string             `json:"mode"`
	ViewerCount int                `json:"viewerCount,omitempty"`
	NowPlaying  *models.NowPlaying `json:"nowPlaying,omitempty"`
}

// GetRooms lists every room with its live state for the lobby.
func GetRooms(w http.ResponseWriter, r *http.Request) {
	hideViewerCount := configrepository.Get().GetHideViewerCount()
	rooms := []viewerRoom{}
	for _, ch := range channelrepository.ListChannels() {
		room := viewerRoom{ID: ch.ID, Name: ch.Name, Mode: ch.EffectiveMode()}
		if c := core.GetChannelRuntime(ch.ID); c != nil {
			status := c.GetStatus()
			room.Online = status.Online
			if !hideViewerCount {
				room.ViewerCount = status.ViewerCount
			}
			if status.Online {
				room.NowPlaying = c.GetNowPlaying()
			}
		}
		rooms = append(rooms, room)
	}
	w.Header().Set("Content-Type", "application/json")
	middleware.DisableCache(w)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"rooms": rooms})
}
