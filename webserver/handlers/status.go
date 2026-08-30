package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"streamingestarr/core"
	"streamingestarr/models"
	"streamingestarr/persistence/configrepository"
	"streamingestarr/utils"
	"streamingestarr/webserver/router/middleware"
	webutils "streamingestarr/webserver/utils"
)

// GetStatus gets the status of the server.
func GetStatus(w http.ResponseWriter, r *http.Request) {
	response := getStatusResponse()

	w.Header().Set("Content-Type", "application/json")
	middleware.DisableCache(w)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		webutils.InternalErrorHandler(w, err)
	}
}

func getStatusResponse() webStatusResponse {
	status := core.GetStatus()
	response := webStatusResponse{
		NowPlaying:         core.Default().GetNowPlaying(),
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
	return response
}

type webStatusResponse struct {
	ServerTime         time.Time       `json:"serverTime"`
	LastConnectTime    *utils.NullTime `json:"lastConnectTime"`
	LastDisconnectTime *utils.NullTime `json:"lastDisconnectTime"`

	NowPlaying    *models.NowPlaying    `json:"nowPlaying,omitempty"`
	Schedule      []models.ScheduleItem `json:"schedule,omitempty"`
	ChannelID     string                `json:"channelId"`
	VersionNumber string                `json:"versionNumber"`
	StreamTitle   string                `json:"streamTitle"`
	ViewerCount   int                   `json:"viewerCount,omitempty"`
	Online        bool                  `json:"online"`
}
