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

// The open stage: a room with ShareIngest on hands its ingest details to
// everyone inside it — the Option-A public relay flow. The response gives
// ports and keys; the client builds addresses from the host it reached us
// on, exactly like the admin page does. Locked rooms keep the details
// behind the same room password as the surface the viewer is on.
type broadcastDetails struct {
	Enabled  bool `json:"enabled"`
	RTMPPort int  `json:"rtmpPort,omitempty"`
	SRT      bool `json:"srt"`
	SRTPort  int  `json:"srtPort,omitempty"`
	TCP      bool `json:"tcp"`
	TCPPort  int  `json:"tcpPort,omitempty"`
	// The keys that open THIS room.
	Keys []models.ChannelKey `json:"keys,omitempty"`
	// Passphrase flag only — the phrase itself stays with the admin.
	// (TCP has no passphrase; TLS is its lock.)
	SRTPassphraseRequired bool `json:"srtPassphraseRequired"`
}

// GetRoomBroadcast returns a room's ingest details for viewers, or
// {enabled:false} when the room keeps its stage closed.
func GetRoomBroadcast(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	middleware.DisableCache(w)

	ch := channelrepository.GetChannel(channelIDFromRequest(r))
	closed := func() { _ = json.NewEncoder(w).Encode(broadcastDetails{Enabled: false}) }
	if ch == nil || !ch.ShareIngest {
		closed()
		return
	}
	// The same lock that guards what the viewer is looking at guards the
	// stage details: relay lock on relay surfaces, theater lock otherwise.
	if ch.Relays() {
		if core.RelayLocked(r, ch) {
			closed()
			return
		}
	} else if core.TheaterLocked(r, ch) {
		closed()
		return
	}

	cfg := configrepository.Get()
	out := broadcastDetails{
		Enabled:               true,
		RTMPPort:              cfg.GetRTMPPortNumber(),
		SRT:                   cfg.GetSRTServerEnabled(),
		SRTPort:               cfg.GetSRTServerPort(),
		TCP:                   cfg.GetTCPIngestEnabled(),
		TCPPort:               cfg.GetTCPIngestPort(),
		SRTPassphraseRequired: ch.Passphrase != "" || cfg.GetSRTPassphrase() != "",
	}
	if ch.ID == channelrepository.DefaultChannelID {
		// The main room's keys are the global list.
		for _, k := range cfg.GetStreamKeys() {
			if k.Key != nil && *k.Key != "" {
				entry := models.ChannelKey{Key: *k.Key}
				if k.Comment != nil {
					entry.Comment = *k.Comment
				}
				out.Keys = append(out.Keys, entry)
			}
		}
	} else {
		out.Keys = ch.Keys
	}
	_ = json.NewEncoder(w).Encode(out)
}
