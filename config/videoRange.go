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
// the 8-bit yuv420p path, which would clip it. Held in the config leaf
// because both the transcoder and the storage/playlist code read it and
// neither may import core.
const (
	VideoRangeSDR = "sdr"
	VideoRangePQ  = "pq"  // HDR10 / PQ (SMPTE ST 2084)
	VideoRangeHLG = "hlg" // Hybrid Log-Gamma (ARIB STD-B67)
)

var (
	_videoRangeLock    sync.RWMutex
	_inboundVideoRange = VideoRangeSDR
)

func init() {
	// Test/stub hook: seed the range before any sender push so the playlist
	// signaling and passthrough guard can be exercised without the sender.
	if v := os.Getenv("STREAMINGESTARR_FORCE_VIDEO_RANGE"); v != "" {
		_inboundVideoRange = NormalizeVideoRange(v)
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

// SetInboundVideoRange records the colour range of the current broadcast.
func SetInboundVideoRange(v string) {
	_videoRangeLock.Lock()
	_inboundVideoRange = NormalizeVideoRange(v)
	_videoRangeLock.Unlock()
}

// GetInboundVideoRange returns the current broadcast's colour range
// (sdr|pq|hlg).
func GetInboundVideoRange() string {
	_videoRangeLock.RLock()
	defer _videoRangeLock.RUnlock()
	return _inboundVideoRange
}

// HLSVideoRangeToken returns the value for the VIDEO-RANGE attribute of an
// EXT-X-STREAM-INF line, or "" for SDR (where the attribute is omitted).
func HLSVideoRangeToken() string {
	switch GetInboundVideoRange() {
	case VideoRangePQ:
		return "PQ"
	case VideoRangeHLG:
		return "HLG"
	default:
		return ""
	}
}
