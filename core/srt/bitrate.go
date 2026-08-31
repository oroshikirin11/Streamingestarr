package srt

import (
	"sync"
	"time"
)

// Live inbound bitrate, measured where it cannot lie: the bytes the read
// loop actually pulled off the socket, sampled every five seconds. This is
// the raw container rate (video+audio+muxing overhead) — the number the
// operator watches to see whether the sender is keeping up.

// BitrateSample is one point of the inbound rate series.
type BitrateSample struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"` // kbps
}

const (
	bitrateSampleEvery = 5 * time.Second
	bitrateKeep        = 360 // 30 minutes of 5s samples
)

var (
	_brMu      sync.Mutex
	_brSamples []BitrateSample
)

// resetBitrate starts a fresh series for a new publisher session.
func resetBitrate() {
	_brMu.Lock()
	_brSamples = nil
	_brMu.Unlock()
}

// recordBitrate appends one sample, trimming the ring.
func recordBitrate(bytes int64, window time.Duration) {
	kbps := float64(bytes) * 8 / window.Seconds() / 1000
	_brMu.Lock()
	_brSamples = append(_brSamples, BitrateSample{Time: time.Now(), Value: kbps})
	if len(_brSamples) > bitrateKeep {
		_brSamples = _brSamples[len(_brSamples)-bitrateKeep:]
	}
	_brMu.Unlock()
}

// GetInboundBitrate returns the current session's rate series.
func GetInboundBitrate() []BitrateSample {
	_brMu.Lock()
	defer _brMu.Unlock()
	out := make([]BitrateSample, len(_brSamples))
	copy(out, _brSamples)
	return out
}
