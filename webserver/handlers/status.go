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
	response := getStatusResponse(channelIDFromRequest(r))

	w.Header().Set("Content-Type", "application/json")
	middleware.DisableCache(w)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		webutils.InternalErrorHandler(w, err)
	}
}

func getStatusResponse(channelID string) webStatusResponse {
	channel := core.GetChannelRuntime(channelID)
	if channel == nil {
		return webStatusResponse{}
	}
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
	return response
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
}
