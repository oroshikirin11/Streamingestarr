package config

import (
	"os"
	"strings"
	"sync"
)

// Inbound HDR signaling. The sender (Jellystreamerr) declares the colour
// range of the broadcast it is pushing so the receiver can (a) advertise it
// in the HLS master playlist via VIDEO-RANGE — without which Safari/native
// HLS renders HDR as washed-out SDR — and (b) refuse to transcode HDR down
// the 8-bit yuv420p path, which would clip it. PER CHANNEL: one room's HDR
// feature must not badge another room's SDR broadcast. Held in the config
// leaf because both the transcoder and the storage/playlist code read it
// and neither may import core.
const (
	VideoRangeSDR = "sdr"
	VideoRangePQ  = "pq"  // HDR10 / PQ (SMPTE ST 2084)
	VideoRangeHLG = "hlg" // Hybrid Log-Gamma (ARIB STD-B67)
)

var (
	_videoRangeLock     sync.RWMutex
	_inboundVideoRanges = map[string]string{}
	// _forcedVideoRange seeds every channel that has no sender push yet —
	// the test/stub hook, see init.
	_forcedVideoRange = ""
)

func init() {
	// Test/stub hook: seed the range before any sender push so the playlist
	// signaling and passthrough guard can be exercised without the sender.
	if v := os.Getenv("STREAMINGESTARR_FORCE_VIDEO_RANGE"); v != "" {
		_forcedVideoRange = NormalizeVideoRange(v)
	}
}

// NormalizeVideoRange maps the aliases a sender might use onto our three
// canonical values, defaulting to SDR for anything unrecognized.
func NormalizeVideoRange(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "pq", "hdr", "hdr10", "hdr10+", "pq10", "smpte2084", "st2084", "bt2100-pq":
		return VideoRangePQ
	case "hlg", "arib-std-b67", "bt2100-hlg":
		return VideoRangeHLG
	default:
		return VideoRangeSDR
	}
}

// SetInboundVideoRange records the colour range of a channel's current
// broadcast.
func SetInboundVideoRange(channelID, v string) {
	_videoRangeLock.Lock()
	_inboundVideoRanges[channelID] = NormalizeVideoRange(v)
	_videoRangeLock.Unlock()
}

// GetInboundVideoRange returns the channel's current colour range
// (sdr|pq|hlg).
func GetInboundVideoRange(channelID string) string {
	_videoRangeLock.RLock()
	defer _videoRangeLock.RUnlock()
	if v, ok := _inboundVideoRanges[channelID]; ok {
		return v
	}
	if _forcedVideoRange != "" {
		return _forcedVideoRange
	}
	return VideoRangeSDR
}

// HLSVideoRangeToken returns the value for the VIDEO-RANGE attribute of an
// EXT-X-STREAM-INF line, or "" for SDR (where the attribute is omitted).
func HLSVideoRangeToken(channelID string) string {
	switch GetInboundVideoRange(channelID) {
	case VideoRangePQ:
		return "PQ"
	case VideoRangeHLG:
		return "HLG"
	default:
		return ""
	}
}
