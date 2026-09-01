package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"streamingestarr/utils"
	"streamingestarr/webserver/router/middleware"
)

// The player incident feed: viewers' players report the moments playback
// misbehaved — backward jumps (repeated words), forward discontinuities,
// stalls, and the sync controller's own snaps — per room. Read next to the
// segment ledger it attributes blame: incidents while the ledger shows
// stretched wall steps = the feed arrived late; incidents over a clean
// ledger = that viewer's network or player.

// PlayerIncident is one misbehaviour one player observed.
type PlayerIncident struct {
	Time     time.Time `json:"time"`
	Client   string    `json:"client"`
	Type     string    `json:"type"` // backjump | forwardjump | stall | sync-snap | hls-error
	MagMs    float64   `json:"magMs,omitempty"`
	PosS     float64   `json:"posS,omitempty"`
	BufferS  float64   `json:"bufferS,omitempty"`
	DriftS   float64   `json:"driftS,omitempty"`
	Rate     float64   `json:"rate,omitempty"`
	Detail   string    `json:"detail,omitempty"`
}

const (
	incidentsKeepPerChannel = 400
	incidentsMaxAge         = 30 * time.Minute
	incidentsMaxPerReport   = 30
)

var (
	_incidentsMu sync.Mutex
	_incidents   = map[string][]PlayerIncident{}
)

func pruneIncidentsLocked(channelID string) {
	list := _incidents[channelID]
	cutoff := time.Now().Add(-incidentsMaxAge)
	i := 0
	for i < len(list) && list[i].Time.Before(cutoff) {
		i++
	}
	list = list[i:]
	if len(list) > incidentsKeepPerChannel {
		list = list[len(list)-incidentsKeepPerChannel:]
	}
	_incidents[channelID] = list
}

// ReportPlayerIncidents ingests a viewer's incident batch (viewer-gated).
func ReportPlayerIncidents(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Channel   string `json:"channel"`
		Incidents []struct {
			T       int64   `json:"t"` // ms epoch, client clock
			Type    string  `json:"type"`
			MagMs   float64 `json:"magMs"`
			PosS    float64 `json:"posS"`
			BufferS float64 `json:"bufferS"`
			DriftS  float64 `json:"driftS"`
			Rate    float64 `json:"rate"`
			Detail  string  `json:"detail"`
		} `json:"incidents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	channelID := payload.Channel
	if channelID == "" {
		channelID = "main"
	}
	client := utils.GenerateClientIDFromRequest(r)
	now := time.Now()

	_incidentsMu.Lock()
	defer _incidentsMu.Unlock()
	n := len(payload.Incidents)
	if n > incidentsMaxPerReport {
		payload.Incidents = payload.Incidents[n-incidentsMaxPerReport:]
	}
	for _, in := range payload.Incidents {
		switch in.Type {
		case "backjump", "forwardjump", "stall", "sync-snap", "hls-error":
		default:
			continue
		}
		if len(in.Detail) > 120 {
			in.Detail = in.Detail[:120]
		}
		// Server-stamped time keeps the feed ordered even with skewed
		// client clocks; the client timestamp only orders within a batch.
		_incidents[channelID] = append(_incidents[channelID], PlayerIncident{
			Time: now, Client: client, Type: in.Type, MagMs: in.MagMs,
			PosS: in.PosS, BufferS: in.BufferS, DriftS: in.DriftS,
			Rate: in.Rate, Detail: in.Detail,
		})
	}
	pruneIncidentsLocked(channelID)
	w.WriteHeader(http.StatusOK)
}

// GetPlayerIncidents returns a room's recent incidents (admin).
func GetPlayerIncidents(w http.ResponseWriter, r *http.Request) {
	channelID := channelIDFromRequest(r)
	_incidentsMu.Lock()
	pruneIncidentsLocked(channelID)
	list := append([]PlayerIncident{}, _incidents[channelID]...)
	_incidentsMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	middleware.DisableCache(w)
	_ = json.NewEncoder(w).Encode(list)
}
