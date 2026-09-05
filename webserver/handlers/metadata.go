package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
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
		// VideoRange declares the broadcast's colour range (sdr|pq|hlg) so the
		// receiver can signal HDR in the HLS master playlist and never
		// transcode it to 8-bit. Optional; absent means unchanged.
		VideoRange string `json:"videoRange,omitempty"`
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
	if payload.VideoRange != "" {
		channel.SetVideoRange(payload.VideoRange)
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

// ResolveChannel tells a sender which room a stream key feeds, so a
// fan-out can push its (identical) metadata once per room WITHOUT any
// room mapping in the sender's settings: it already holds the keys.
// Body: {"key": "..."} → {"channel": "<room id>"}.
func ResolveChannel(integration models.ExternalAPIUser, w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Key == "" {
		webutils.BadRequestHandler(w, errors.New("send {\"key\": \"...\"}"))
		return
	}
	channelID := channelrepository.GetChannelIDForKey(payload.Key)
	if channelID == "" {
		// Not a room key — check the global list, which feeds the default
		// channel. An unknown key resolves to nothing, honestly.
		channelID = channelrepository.DefaultChannelID
		found := false
		for _, k := range configrepository.Get().GetStreamKeys() {
			if k.Key != nil && *k.Key != "" && *k.Key == payload.Key {
				found = true
				break
			}
		}
		if !found {
			webutils.WriteSimpleResponse(w, false, "unknown stream key")
			return
		}
	}
	// The room's mode rides along: a relay room wants H.264 SDR from the
	// sender, so it can decide the codec before going live.
	response := map[string]interface{}{"channel": channelID}
	if ch := channelrepository.GetChannel(channelID); ch != nil {
		response["name"] = ch.Name
		response["mode"] = ch.EffectiveMode()
		response["relay"] = map[string]interface{}{
			"enabled":   ch.Relays(),
			"protocols": ch.EffectiveRelayProtocols(),
			"wants":     map[string]string{"video": "h264", "range": "sdr"},
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// GetCapabilities tells a sender what this receiver accepts, so a
// "Streamingestarr mode" can configure itself.
func GetCapabilities(integration models.ExternalAPIUser, w http.ResponseWriter, r *http.Request) {
	configRepository := configrepository.Get()
	channels := []string{}
	rooms := []map[string]interface{}{}
	for _, c := range channelrepository.ListChannels() {
		channels = append(channels, c.ID)
		rooms = append(rooms, map[string]interface{}{"id": c.ID, "name": c.Name, "mode": c.EffectiveMode(), "relay": c.Relays()})
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
		// Rooms with their mode; a relay room takes H.264 SDR only (the
		// resolve-channel answer says so per stream key as well).
		"rooms": rooms,
		"relay": map[string]interface{}{"rtspPort": configRepository.GetRelayRTSPPort(), "wants": map[string]string{"video": "h264", "range": "sdr"}},
		"metadata": map[string]bool{
			"nowPlaying": true,
			"schedule":   true,
			"artwork":    true,
			"videoRange": true,
		},
		// HDR: declare "videoRange" on the nowPlaying push. pq = HDR10/PQ,
		// hlg = HLG; anything else is treated as SDR. HDR forces passthrough,
		// so send the original 10-bit HEVC/AV1 bitstream (over Matroska/fMP4
		// for AV1 — mpegts can't carry it) and disable any transcode ladder.
		"videoRange": map[string]interface{}{
			"accepts": []string{"sdr", "pq", "hlg"},
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
