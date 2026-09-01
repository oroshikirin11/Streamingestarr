package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"streamingestarr/config"
	"streamingestarr/core/rtmp"
	"streamingestarr/core/srt"
	"streamingestarr/core/transcoder"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/utils"
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
	nowPlaying       *models.NowPlaying
	lastPlayed       *models.LastPlayed

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

// TotalViewerCount sums the live viewers of every room — the number the
// viewers-over-time metric records now that rooms broadcast concurrently.
func TotalViewerCount() int {
	_channelsLock.RLock()
	defer _channelsLock.RUnlock()
	total := 0
	for _, c := range _channels {
		s := c.GetStatus()
		if s.Online {
			total += s.ViewerCount
		}
	}
	return total
}

// AnyChannelOnline reports whether at least one room is broadcasting.
func AnyChannelOnline() bool {
	_channelsLock.RLock()
	defer _channelsLock.RUnlock()
	for _, c := range _channels {
		if c.GetStatus().Online {
			return true
		}
	}
	return false
}

// isStreamKeyBusy reports whether the channel a stream key feeds already
// has a live inbound stream — shared by the RTMP and SRT listeners so the
// two protocols cannot overtake each other.
func isStreamKeyBusy(streamKey string) bool {
	c := ChannelRuntimeForStreamKey(streamKey)
	return c != nil && c.stats != nil && c.stats.StreamConnected
}

// ChannelRuntimeForStreamKey resolves which channel an inbound stream with
// the given key feeds: a key owned by a room routes there, anything else —
// the global key list — feeds the default channel. This one lookup is what
// lets every room share the same three ingest ports.
func ChannelRuntimeForStreamKey(streamKey string) *ChannelRuntime {
	if id := channelrepository.GetChannelIDForKey(streamKey); id != "" {
		if c := GetChannelRuntime(id); c != nil {
			return c
		}
	}
	return Default()
}

// AddChannel creates a new room end to end: the data row (with a freshly
// minted stream key), its runtime, and its offline state — live, no restart.
func AddChannel(id, name string) (*models.Channel, error) {
	key, err := utils.GenerateAccessToken()
	if err != nil {
		return nil, err
	}
	if err := channelrepository.AddChannel(id, name, key); err != nil {
		return nil, err
	}

	c := newChannelRuntime(id)
	_channelsLock.Lock()
	_channels[id] = c
	_channelsLock.Unlock()

	if err := c.start(); err != nil {
		// Roll back so a half-born room doesn't haunt the list.
		_channelsLock.Lock()
		delete(_channels, id)
		_channelsLock.Unlock()
		_ = channelrepository.DeleteChannel(id)
		return nil, err
	}
	return channelrepository.GetChannel(id), nil
}

// RemoveChannel tears a room down: disconnects any live inbound stream,
// stops its timers, drops the runtime and deletes the row and HLS data.
func RemoveChannel(id string) error {
	if id == channelrepository.DefaultChannelID {
		return fmt.Errorf("the default room cannot be deleted")
	}
	c := GetChannelRuntime(id)
	if c == nil {
		return fmt.Errorf("no such room")
	}

	if err := channelrepository.DeleteChannel(id); err != nil {
		return err
	}
	_channelsLock.Lock()
	delete(_channels, id)
	_channelsLock.Unlock()

	// A live broadcast ends via the normal ingest teardown — closing the
	// connection makes the transcoder finish, which runs the room's own
	// disconnect path. Everything after that is asynchronous, so the disk
	// cleanup waits it out in the background and NEVER uses the fatal-happy
	// cleanup helper: a still-writing transcoder losing a race against
	// directory removal must cost a retry, not the process.
	rtmp.Disconnect(id)
	srt.Disconnect(id)
	c.StopOfflineCleanupTimer()
	c.stopOnlineCleanupTimer()

	go func() {
		for attempt := 0; attempt < 10; attempt++ {
			time.Sleep(2 * time.Second)
			if err := os.RemoveAll(c.HLSOutputPath); err == nil {
				return
			}
		}
		log.Warnln("unable to fully remove the HLS data of deleted room", id)
	}()
	return nil
}
