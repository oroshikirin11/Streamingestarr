package core

import (
	"path/filepath"
	"sync"
	"time"

	"streamingestarr/config"
	"streamingestarr/core/transcoder"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
)

// ChannelRuntime is the live state of one channel (theater): its stream
// stats, storage, transcoder, broadcaster and timers. Nothing here is a
// package-level singleton — the map below is keyed by channel ID so a
// second theater is a data row, not a refactor (docs/design.md §8).
type ChannelRuntime struct {
	ID string
	// HLSOutputPath is this channel's segment/playlist directory:
	// data/hls/<channel id>.
	HLSOutputPath string

	stats            *models.Stats
	storage          models.StorageProvider
	transcoder       *transcoder.Transcoder
	broadcaster      *models.Broadcaster
	currentBroadcast *models.CurrentBroadcast

	offlineCleanupTimer *time.Timer
	onlineCleanupTicker *time.Ticker

	handler    transcoder.HLSHandler
	fileWriter transcoder.FileWriterReceiverService

	viewersLock sync.RWMutex
}

var (
	_channels     = map[string]*ChannelRuntime{}
	_channelsLock sync.RWMutex
)

func newChannelRuntime(id string) *ChannelRuntime {
	return &ChannelRuntime{
		ID:            id,
		HLSOutputPath: filepath.Join(config.HLSStoragePath, id),
	}
}

// Default returns the default channel's runtime. Package-level helpers and
// the pre-multichannel API surface delegate here.
func Default() *ChannelRuntime {
	return GetChannelRuntime(channelrepository.DefaultChannelID)
}

// GetChannelRuntime returns the runtime for a channel ID, or nil.
func GetChannelRuntime(id string) *ChannelRuntime {
	_channelsLock.RLock()
	defer _channelsLock.RUnlock()
	return _channels[id]
}

// ChannelRuntimeForStreamKey resolves which channel an inbound stream with
// the given key feeds. Stream keys are a single global list today, so every
// key feeds the default channel; when keys grow a channel column this is
// the one place that changes.
func ChannelRuntimeForStreamKey(_ string) *ChannelRuntime {
	return Default()
}
