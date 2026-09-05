package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"streamingestarr/config"
	"streamingestarr/core"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/persistence/configrepository"
	"streamingestarr/utils"
	"streamingestarr/webserver/router/middleware"
	webutils "streamingestarr/webserver/utils"
)

// channelIDFromRequest resolves the optional ?channel= query parameter,
// falling back to the default channel for unknown or absent values.
func channelIDFromRequest(r *http.Request) string {
	id := r.URL.Query().Get("channel")
	if id == "" || channelrepository.GetChannel(id) == nil {
		return channelrepository.DefaultChannelID
	}
	return id
}

// GetStatus gets the status of the server. ?channel= scopes the response
// to one room; absent (or unknown) it describes the default room, exactly
// as before rooms existed.
func GetStatus(w http.ResponseWriter, r *http.Request) {
	response := getStatusResponse(channelIDFromRequest(r), r)

	w.Header().Set("Content-Type", "application/json")
	middleware.DisableCache(w)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		webutils.InternalErrorHandler(w, err)
	}
}

func getStatusResponse(channelID string, r *http.Request) webStatusResponse {
	channel := core.GetChannelRuntime(channelID)
	if channel == nil {
		return webStatusResponse{}
	}
	room := channelrepository.GetChannel(channelID)
	status := channel.GetStatus()
	response := webStatusResponse{
		NowPlaying:         channel.GetNowPlaying(),
		LastPlayed:         channel.GetLastPlayed(),
		Schedule:           core.GetSchedule(),
		ChannelID:          status.ChannelID,
		Online:             status.Online,
		ServerTime:         time.Now(),
		LastConnectTime:    status.LastConnectTime,
		LastDisconnectTime: status.LastDisconnectTime,
		VersionNumber:      status.VersionNumber,
		StreamTitle:        status.StreamTitle,
	}
	configRepository := configrepository.Get()
	if !configRepository.GetHideViewerCount() {
		response.ViewerCount = status.ViewerCount
	}
	// The colour range the sender declared ("pq"/"hlg"), so the viewer UI
	// can badge HDR broadcasts. Omitted for SDR — absence means nothing to
	// announce, same as before the field existed.
	if status.Online {
		if vr := config.HLSVideoRangeToken(channelID); vr != "" {
			response.VideoRange = vr
		}
	}
	enabled, controlConnected := channel.PauseVoteInfo()
	response.PauseVote = &pauseVoteStatus{Enabled: enabled, ControlConnected: controlConnected}

	// The room's mode and doors, as they stand for THIS session: a lock
	// reads open once the session has entered the room's password.
	if room != nil {
		response.Mode = room.EffectiveMode()
		response.Access = &roomAccessStatus{
			PasswordSet:   room.RoomPasswordHash != "",
			TheaterLocked: room.HasTheater() && core.TheaterLocked(r, room),
			RelayLocked:   room.Relays() && core.RelayLocked(r, room),
		}
		if room.Relays() {
			rs := &relayStatus{Protocols: room.EffectiveRelayProtocols(), Players: channel.RelayPlayerCount()}
			if !response.Access.RelayLocked {
				rs.Links = core.RelayLinks(r, room)
			}
			response.Relay = rs
		}
	}
	return response
}

// roomAccessStatus is what the room asks of this session.
type roomAccessStatus struct {
	// PasswordSet: the room has its own password at all.
	PasswordSet bool `json:"passwordSet"`
	// TheaterLocked: the player page needs it and this session has not entered it.
	TheaterLocked bool `json:"theaterLocked"`
	// RelayLocked: the relay links need it and this session has not entered it.
	RelayLocked bool `json:"relayLocked"`
}

// relayStatus is the room's relay as the viewer sees it. Links are absent
// while the relay is locked for this session.
type relayStatus struct {
	Protocols []string          `json:"protocols"`
	Links     map[string]string `json:"links,omitempty"`
	Players   int               `json:"players"`
}

// pauseVoteStatus is the room's side of viewer pause votes; the sender's
// side (controls, pausedBy, pausedAt, pending) rides on nowPlaying.
type pauseVoteStatus struct {
	// Enabled is the room's admin switch.
	Enabled bool `json:"enabled"`
	// ControlConnected is whether a sender control connection is up.
	ControlConnected bool `json:"controlConnected"`
}

type webStatusResponse struct {
	ServerTime         time.Time       `json:"serverTime"`
	LastConnectTime    *utils.NullTime `json:"lastConnectTime"`
	LastDisconnectTime *utils.NullTime `json:"lastDisconnectTime"`

	NowPlaying    *models.NowPlaying    `json:"nowPlaying,omitempty"`
	LastPlayed    *models.LastPlayed    `json:"lastPlayed,omitempty"`
	Schedule      []models.ScheduleItem `json:"schedule,omitempty"`
	ChannelID     string                `json:"channelId"`
	VersionNumber string                `json:"versionNumber"`
	StreamTitle   string                `json:"streamTitle"`
	ViewerCount   int                   `json:"viewerCount,omitempty"`
	Online        bool                  `json:"online"`
	VideoRange    string                `json:"videoRange,omitempty"` // "PQ" | "HLG", absent for SDR
	PauseVote     *pauseVoteStatus      `json:"pauseVote,omitempty"`
	// Mode is theater, relay or both; Access and Relay describe the doors
	// and the links for this session.
	Mode   string            `json:"mode,omitempty"`
	Access *roomAccessStatus `json:"access,omitempty"`
	Relay  *relayStatus      `json:"relay,omitempty"`
}
