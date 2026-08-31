package config

import "sync"

// Inbound audio codec, sniffed from the stream head by the SRT ingest
// before the transcoder spawns. The fMP4 path has to choose a bitstream
// filter per audio codec at spawn time, and choosing wrong is fatal to the
// session — so the ingest records what actually arrived and the transcoder
// reads it here. Empty means unknown (RTMP, or a failed sniff), which the
// transcoder treats as AAC — the only audio RTMP delivers here. Held in the
// config leaf for the same reason as the video range: the transcoder may
// not import core.
var (
	_audioCodecLock    sync.RWMutex
	_inboundAudioCodec = ""
)

// SetInboundAudioCodec records the audio codec of the current broadcast
// (ffprobe's lowercase name: "aac", "opus", "eac3", …), or "" for unknown.
func SetInboundAudioCodec(v string) {
	_audioCodecLock.Lock()
	_inboundAudioCodec = v
	_audioCodecLock.Unlock()
}

// GetInboundAudioCodec returns the current broadcast's audio codec, or ""
// when it was never sniffed.
func GetInboundAudioCodec() string {
	_audioCodecLock.RLock()
	defer _audioCodecLock.RUnlock()
	return _inboundAudioCodec
}
