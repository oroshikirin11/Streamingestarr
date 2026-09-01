package admin

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"
	"streamingestarr/core"
	"streamingestarr/metrics"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/webserver/router/middleware"
)

// Status gets the details of a room's inbound broadcaster. ?channel=
// scopes it; the default room otherwise.
func Status(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel")
	if channelID == "" || channelrepository.GetChannel(channelID) == nil {
		channelID = channelrepository.DefaultChannelID
	}
	channel := core.GetChannelRuntime(channelID)
	if channel == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	broadcaster := channel.GetBroadcaster()
	status := channel.GetStatus()
	currentBroadcast := channel.GetCurrentBroadcast()
	health := metrics.GetStreamHealthOverview()
	response := adminStatusResponse{
		Broadcaster:            broadcaster,
		CurrentBroadcast:       currentBroadcast,
		Online:                 status.Online,
		Health:                 health,
		ViewerCount:            status.ViewerCount,
		OverallPeakViewerCount: status.OverallMaxViewerCount,
		SessionPeakViewerCount: status.SessionMaxViewerCount,
		VersionNumber:          status.VersionNumber,
		StreamTitle:            channelrepository.GetEffectiveStreamTitle(channelID),
	}

	w.Header().Set("Content-Type", "application/json")
	middleware.DisableCache(w)

	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		log.Errorln(err)
	}
}

type adminStatusResponse struct {
	Broadcaster            *models.Broadcaster          `json:"broadcaster"`
	CurrentBroadcast       *models.CurrentBroadcast     `json:"currentBroadcast"`
	Health                 *models.StreamHealthOverview `json:"health"`
	StreamTitle            string                       `json:"streamTitle"`
	VersionNumber          string                       `json:"versionNumber"`
	ViewerCount            int                          `json:"viewerCount"`
	OverallPeakViewerCount int                          `json:"overallPeakViewerCount"`
	SessionPeakViewerCount int                          `json:"sessionPeakViewerCount"`
	Online                 bool                         `json:"online"`
}
