package models

// Channel is one theater: an ingest target with its own stream state, HLS
// output and (eventually) chat room. The service ships with exactly one,
// but everything downstream is keyed by channel so a second one is a data
// row, not a rework (docs/design.md §8).
type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Keys are the room's own inbound stream keys — how a stream on the
	// shared ingest ports finds this room. A room can hold any number of
	// them, mirroring the global list. Empty for the default channel, whose
	// keys are the global stream-key list.
	Keys []ChannelKey `json:"keys,omitempty"`

	// Per-room broadcast configuration. Zero values mean "inherit the
	// server defaults" (the Video/Settings sections): title "", welcome "",
	// latency -1, segment format "", variants nil.
	Title          string                `json:"title,omitempty"`
	WelcomeMessage string                `json:"welcomeMessage,omitempty"`
	LatencyLevel   int                   `json:"latencyLevel"`
	SegmentFormat  string                `json:"segmentFormat,omitempty"`
	OutputVariants []StreamOutputVariant `json:"outputVariants,omitempty"`
}

// ChannelKey is one stream key a room accepts, with an optional comment —
// the same shape the global key list uses.
type ChannelKey struct {
	Key     string `json:"key"`
	Comment string `json:"comment,omitempty"`
}
