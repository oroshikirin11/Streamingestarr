package handlers

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

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
			"artwork":    true,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// SetArtworkMetadata stores a poster image under a sender-chosen id.
// Body: {"id": "...", "type": "image/jpeg|png|webp", "data": "<base64>"}.
func SetArtworkMetadata(integration models.ExternalAPIUser, w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		webutils.BadRequestHandler(w, err)
		return
	}
	payload.ID = strings.TrimSpace(payload.ID)
	if payload.ID == "" || len(payload.ID) > 128 {
		webutils.WriteSimpleResponse(w, false, "artwork id must be 1-128 characters")
		return
	}
	switch payload.Type {
	case "image/jpeg", "image/png", "image/webp":
	default:
		webutils.WriteSimpleResponse(w, false, "type must be image/jpeg, image/png or image/webp")
		return
	}
	data, err := base64.StdEncoding.DecodeString(payload.Data)
	if err != nil || len(data) == 0 {
		webutils.WriteSimpleResponse(w, false, "data must be non-empty base64")
		return
	}
	if len(data) > 1<<20 {
		webutils.WriteSimpleResponse(w, false, "artwork too large (max 1 MiB)")
		return
	}
	core.SetArtwork(payload.ID, payload.Type, data)
	webutils.WriteSimpleResponse(w, true, "artwork stored")
}

// GetArtwork serves a stored poster to viewers.
func GetArtwork(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/artwork/")
	data, contentType := core.GetArtwork(id)
	if data == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	// ids are sender-versioned, so cache hard.
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	_, _ = w.Write(data)
}
