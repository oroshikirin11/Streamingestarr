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
	enabled, controlConnected := channel.PauseVoteInfo()
	response.PauseVote = adminPauseVoteStatus{Enabled: enabled, ControlConnected: controlConnected}
	if np := channel.GetNowPlaying(); np != nil {
		response.PauseVote.Paused = np.Paused
		response.PauseVote.PausedBy = np.PausedBy
		response.PauseVote.PausedAt = np.PausedAt
		response.PauseVote.Pending = np.Pending
		response.PauseVote.Advertised = np.Controls != nil && np.Controls.PauseVote
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
	PauseVote              adminPauseVoteStatus         `json:"pauseVote"`
}

// adminPauseVoteStatus is the viewer pause-vote picture for the admin:
// the room switch, the sender connection, and who paused when.
type adminPauseVoteStatus struct {
	Enabled          bool   `json:"enabled"`
	ControlConnected bool   `json:"controlConnected"`
	Advertised       bool   `json:"advertised"`
	Paused           bool   `json:"paused"`
	PausedBy         string `json:"pausedBy"`
	PausedAt         int64  `json:"pausedAt"`
	Pending          string `json:"pending"`
}
