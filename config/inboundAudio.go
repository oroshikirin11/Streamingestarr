package config

import "sync"

// Inbound codecs, sniffed from the stream head by the SRT ingest before the
// transcoder spawns. Two consumers, both of which must not guess:
//
//   - The fMP4 path has to choose a bitstream filter per AUDIO codec at
//     spawn time, and choosing wrong is fatal to the session.
//   - ffmpeg 9's hls muxer writes the master playlist with a DEFAULT avc1
//     CODECS string when a copied VIDEO stream's parameters are not yet
//     known — a lie that makes hls.js init an H.264 decoder against AV1
//     segments (sound, no picture). The playlist repair compares the claim
//     against what actually arrived.
//
// Empty means unknown (RTMP, or a failed sniff): the audio consumer treats
// that as AAC — the only audio RTMP delivers here — and the playlist repair
// leaves the master alone. Held in the config leaf for the same reason as
// the video range: neither consumer may import core.
var (
	_inboundCodecsLock sync.RWMutex
	_inboundAudioCodec = ""
	_inboundVideoCodec = ""
)

// SetInboundAudioCodec records the audio codec of the current broadcast
// (ffprobe's lowercase name: "aac", "opus", "eac3", …), or "" for unknown.
func SetInboundAudioCodec(v string) {
	_inboundCodecsLock.Lock()
	_inboundAudioCodec = v
	_inboundCodecsLock.Unlock()
}

// GetInboundAudioCodec returns the current broadcast's audio codec, or ""
// when it was never sniffed.
func GetInboundAudioCodec() string {
	_inboundCodecsLock.RLock()
	defer _inboundCodecsLock.RUnlock()
	return _inboundAudioCodec
}

// SetInboundVideoCodec records the video codec of the current broadcast
// (ffprobe's lowercase name: "h264", "hevc", "av1", …), or "" for unknown.
func SetInboundVideoCodec(v string) {
	_inboundCodecsLock.Lock()
	_inboundVideoCodec = v
	_inboundCodecsLock.Unlock()
}

// GetInboundVideoCodec returns the current broadcast's video codec, or ""
// when it was never sniffed.
func GetInboundVideoCodec() string {
	_inboundCodecsLock.RLock()
	defer _inboundCodecsLock.RUnlock()
	return _inboundVideoCodec
}
