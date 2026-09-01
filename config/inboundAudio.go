package config

import "sync"

// Inbound codecs, sniffed from the stream head by the ingest before the
// transcoder spawns — PER CHANNEL, because rooms broadcast concurrently and
// one room's AV1 must not reroute another room's H.264. Two consumers, both
// of which must not guess:
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
	_inboundCodecsLock  sync.RWMutex
	_inboundAudioCodecs = map[string]string{}
	_inboundVideoCodecs = map[string]string{}
)

// SetInboundAudioCodec records the audio codec of a channel's current
// broadcast (ffprobe's lowercase name: "aac", "opus", "eac3", …), or "" for
// unknown.
func SetInboundAudioCodec(channelID, v string) {
	_inboundCodecsLock.Lock()
	_inboundAudioCodecs[channelID] = v
	_inboundCodecsLock.Unlock()
}

// GetInboundAudioCodec returns the channel's current audio codec, or ""
// when it was never sniffed.
func GetInboundAudioCodec(channelID string) string {
	_inboundCodecsLock.RLock()
	defer _inboundCodecsLock.RUnlock()
	return _inboundAudioCodecs[channelID]
}

// SetInboundVideoCodec records the video codec of a channel's current
// broadcast (ffprobe's lowercase name: "h264", "hevc", "av1", …), or "" for
// unknown.
func SetInboundVideoCodec(channelID, v string) {
	_inboundCodecsLock.Lock()
	_inboundVideoCodecs[channelID] = v
	_inboundCodecsLock.Unlock()
}

// GetInboundVideoCodec returns the channel's current video codec, or ""
// when it was never sniffed.
func GetInboundVideoCodec(channelID string) string {
	_inboundCodecsLock.RLock()
	defer _inboundCodecsLock.RUnlock()
	return _inboundVideoCodecs[channelID]
}
