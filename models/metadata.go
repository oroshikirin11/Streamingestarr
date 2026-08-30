package models

import "time"

// NowPlaying is the structured now-playing metadata a sender (Jellystreamerr)
// pushes over the integrations API — the replacement for abusing the stream
// title string. See docs/integration-jellystreamerr.md for the contract.
type NowPlaying struct {
	// Title is the work: series or film name.
	Title string `json:"title"`
	// Subtitle is the specifics: "S1E4 — eps1.3_da3m0ns.mp4".
	Subtitle string `json:"subtitle,omitempty"`
	// ArtworkID references an image previously pushed to the artwork
	// endpoint; viewers fetch it at /artwork/<id>.
	ArtworkID string `json:"artworkId,omitempty"`
	// UpNext is what follows, if the sender knows.
	UpNext *UpNextItem `json:"upNext,omitempty"`
	// Position/Duration in seconds let the viewer render true progress;
	// the receiver stamps ReceivedAt so clients can extrapolate.
	Position   float64   `json:"position,omitempty"`
	Duration   float64   `json:"duration,omitempty"`
	ReceivedAt time.Time `json:"receivedAt"`
	// Paused freezes client-side position extrapolation.
	Paused bool `json:"paused,omitempty"`
	// Announce asks the receiver to post a system chat line on change.
	Announce bool `json:"announce,omitempty"`
}

// LastPlayed remembers what the room watched most recently — shown in
// the lobby after the stream ends.
type LastPlayed struct {
	Title    string    `json:"title"`
	Subtitle string    `json:"subtitle,omitempty"`
	EndedAt  time.Time `json:"endedAt"`
}

// UpNextItem describes the next queued item.
type UpNextItem struct {
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	ArtworkID string `json:"artworkId,omitempty"`
}

// ScheduleItem is one upcoming showing, powering the lobby countdown.
type ScheduleItem struct {
	Title     string    `json:"title"`
	Subtitle  string    `json:"subtitle,omitempty"`
	ArtworkID string    `json:"artworkId,omitempty"`
	StartsAt  time.Time `json:"startsAt"`
}
