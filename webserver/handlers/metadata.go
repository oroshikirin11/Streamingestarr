package handlers

import (
	"encoding/json"
	"net/http"

	"streamingestarr/core"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/persistence/configrepository"
	webutils "streamingestarr/webserver/utils"
)

// The structured metadata channel (docs/integration-jellystreamerr.md):
// the sender pushes now-playing and schedule data over the integrations
// API so the theater page can render real information instead of a title
// string.

// SetNowPlayingMetadata stores now-playing metadata for a channel.
func SetNowPlayingMetadata(integration models.ExternalAPIUser, w http.ResponseWriter, r *http.Request) {
	var payload struct {
		models.NowPlaying
		Channel string `json:"channel,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		webutils.BadRequestHandler(w, err)
		return
	}
	channelID := payload.Channel
	if channelID == "" {
		channelID = channelrepository.DefaultChannelID
	}
	channel := core.GetChannelRuntime(channelID)
	if channel == nil {
		webutils.WriteSimpleResponse(w, false, "unknown channel")
		return
	}
	channel.SetNowPlaying(payload.NowPlaying)
	webutils.WriteSimpleResponse(w, true, "now playing updated")
}

// SetScheduleMetadata replaces the upcoming-showings schedule.
func SetScheduleMetadata(integration models.ExternalAPIUser, w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Items []models.ScheduleItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		webutils.BadRequestHandler(w, err)
		return
	}
	core.SetSchedule(payload.Items)
	webutils.WriteSimpleResponse(w, true, "schedule updated")
}

// GetCapabilities tells a sender what this receiver accepts, so a
// "Streamingestarr mode" can configure itself.
func GetCapabilities(integration models.ExternalAPIUser, w http.ResponseWriter, r *http.Request) {
	configRepository := configrepository.Get()
	channels := []string{}
	for _, c := range channelrepository.ListChannels() {
		channels = append(channels, c.ID)
	}
	response := map[string]interface{}{
		"service":    "streamingestarr",
		"apiVersion": 1,
		"ingest": map[string]interface{}{
			"rtmpPort":   configRepository.GetRTMPPortNumber(),
			"srtEnabled": configRepository.GetSRTServerEnabled(),
			"srtPort":    configRepository.GetSRTServerPort(),
			// AV1 must ride Matroska or fMP4 over SRT; mpegts cannot carry it.
			"srtContainers": []string{"mpegts", "matroska", "mp4"},
		},
		"segmentFormat": configRepository.GetVideoSegmentFormat(),
		"channels":      channels,
		"metadata": map[string]bool{
			"nowPlaying": true,
			"schedule":   true,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
