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

	// Passphrase is the room's own SRT passphrase. When set it replaces
	// the global SRT passphrase for streams that open this room over SRT
	// with one of its keys; empty inherits the global one. (TCP has no
	// passphrase — TLS is its lock.) Never serialised: the admin API
	// reports only whether one is set.
	Passphrase string `json:"-"`

	// PauseVoteEnabled is the room's own switch for viewer pause votes
	// (default on). Votes also need the sender to advertise the control
	// and to hold a control connection.
	PauseVoteEnabled bool `json:"pauseVoteEnabled"`

	// Mode is what the room is: a theater (the player page), a relay
	// (links for external players such as VRChat, no theater), or both.
	// Stored "" reads as theater.
	Mode string `json:"mode"`
	// RelayProtocols are the link kinds the relay offers, in the order
	// the card shows them: rtsp, ts, hls. Empty means rtsp only.
	RelayProtocols []string `json:"relayProtocols"`
	// RelayToken is the secret in every relay link. Rotating it ends
	// every link at once. Never serialised through the model — the admin
	// and viewer surfaces decide who sees the links.
	RelayToken string `json:"-"`
	// ShareIngest is the "open stage" switch: when on, everyone inside
	// the room (behind its locks) sees the ingest address and stream keys
	// and may broadcast here. The admin's New key is the revocation lever.
	ShareIngest bool `json:"shareIngest"`

	// RoomPasswordHash is the room's own password, hashed; "" means none.
	// LockTheater and LockRelay say what it guards. Both are off when no
	// password is set.
	RoomPasswordHash string `json:"-"`
	LockTheater      bool   `json:"lockTheater"`
	LockRelay        bool   `json:"lockRelay"`
}

// Room modes.
const (
	RoomModeTheater = "theater"
	RoomModeRelay   = "relay"
	RoomModeBoth    = "both"
)

// Relay protocols, in display order.
const (
	RelayProtocolRTSP = "rtsp"
	RelayProtocolTS   = "ts"
	RelayProtocolHLS  = "hls"
)

// RelayProtocolOrder is the canonical order of relay link kinds.
var RelayProtocolOrder = []string{RelayProtocolRTSP, RelayProtocolTS, RelayProtocolHLS}

// EffectiveMode reads the stored mode with "" as theater.
func (c Channel) EffectiveMode() string {
	switch c.Mode {
	case RoomModeRelay, RoomModeBoth:
		return c.Mode
	}
	return RoomModeTheater
}

// Relays reports whether the room offers relay links at all.
func (c Channel) Relays() bool {
	m := c.EffectiveMode()
	return m == RoomModeRelay || m == RoomModeBoth
}

// HasTheater reports whether the room's player page is open to viewers.
func (c Channel) HasTheater() bool {
	return c.EffectiveMode() != RoomModeRelay
}

// EffectiveRelayProtocols is the stored list normalised: known kinds
// only, canonical order, rtsp when nothing valid is stored.
func (c Channel) EffectiveRelayProtocols() []string {
	out := []string{}
	for _, want := range RelayProtocolOrder {
		for _, have := range c.RelayProtocols {
			if have == want {
				out = append(out, want)
				break
			}
		}
	}
	if len(out) == 0 {
		out = append(out, RelayProtocolRTSP)
	}
	return out
}

// ChannelKey is one stream key a room accepts, with an optional comment —
// the same shape the global key list uses.
type ChannelKey struct {
	Key     string `json:"key"`
	Comment string `json:"comment,omitempty"`
}
