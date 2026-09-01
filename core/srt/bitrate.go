package srt

import (
	"time"

	gosrt "github.com/datarhei/gosrt"
)

// Live inbound bitrate, measured where it cannot lie: the bytes the read
// loop actually pulled off the socket, sampled every five seconds. This is
// the raw container rate (video+audio+muxing overhead) — the number the
// operator watches to see whether the sender is keeping up. One ring per
// session, so concurrent rooms never mix their numbers.

// BitrateSample is one point of the inbound rate series.
type BitrateSample struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"` // kbps
}

const (
	bitrateSampleEvery = 5 * time.Second
	bitrateKeep        = 360 // 30 minutes of 5s samples
)

// recordBitrate appends one sample to the session's ring, trimming it.
func (s *ingestSession) recordBitrate(bytes int64, window time.Duration) {
	kbps := float64(bytes) * 8 / window.Seconds() / 1000
	s.brMu.Lock()
	s.brSamples = append(s.brSamples, BitrateSample{Time: time.Now(), Value: kbps})
	if len(s.brSamples) > bitrateKeep {
		s.brSamples = s.brSamples[len(s.brSamples)-bitrateKeep:]
	}
	s.brMu.Unlock()
}

// GetInboundBitrate returns the channel's current rate series — the live
// session's, or the last finished one's so the graph survives a disconnect.
func GetInboundBitrate(channelID string) []BitrateSample {
	_sessionsMu.Lock()
	s := _sessions[channelID]
	if s == nil {
		s = _lastSessions[channelID]
	}
	_sessionsMu.Unlock()
	if s == nil {
		return []BitrateSample{}
	}
	s.brMu.Lock()
	defer s.brMu.Unlock()
	out := make([]BitrateSample, len(s.brSamples))
	copy(out, s.brSamples)
	return out
}

// IngestStats is the receive-health surface for the admin: the SRT
// counters that convict (or acquit) the transport when viewers report
// artifacts, plus our own buffer's overflow counter. PktRecvDrop is the
// smoking gun — packets SRT gave up on because nobody consumed in time.
type IngestStats struct {
	Connected          bool   `json:"connected"`
	SrtPktRecvDrop     uint64 `json:"srtPktRecvDrop"`
	SrtPktRecvLoss     uint64 `json:"srtPktRecvLoss"`
	SrtPktRecvRetrans  uint64 `json:"srtPktRecvRetrans"`
	BufferDroppedBytes int64  `json:"bufferDroppedBytes"`
}

// GetIngestStats reads the channel's live receive statistics; after a
// disconnect the last session's overflow counter stays visible.
func GetIngestStats(channelID string) IngestStats {
	_sessionsMu.Lock()
	s := _sessions[channelID]
	last := _lastSessions[channelID]
	_sessionsMu.Unlock()

	out := IngestStats{}
	if s == nil {
		if last != nil {
			out.BufferDroppedBytes = last.queueDroppedBytes.Load()
		}
		return out
	}
	out.Connected = true
	out.BufferDroppedBytes = s.queueDroppedBytes.Load()
	// SRT counters only exist on an SRT session; a TCP ingest cannot lose
	// packets by construction (the kernel retransmits), so zeros are truth.
	if c, ok := s.conn.(gosrt.Conn); ok {
		var st gosrt.Statistics
		c.Stats(&st)
		out.SrtPktRecvDrop = st.Accumulated.PktRecvDrop
		out.SrtPktRecvLoss = st.Accumulated.PktRecvLoss
		out.SrtPktRecvRetrans = st.Accumulated.PktRecvRetrans
	}
	return out
}
