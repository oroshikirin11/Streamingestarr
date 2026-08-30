package core

import (
	"fmt"
	"sync"
	"time"

	"streamingestarr/core/chat"
	"streamingestarr/models"
)

// Now-playing metadata lives on the channel runtime (cleared when the
// stream ends); the schedule is channel-agnostic for now and survives in
// memory only until the sender re-pushes — senders own the schedule.
var (
	_metadataLock sync.RWMutex
	_schedule     []models.ScheduleItem

	_artwork      = map[string]artworkEntry{}
	_artworkOrder []string
)

type artworkEntry struct {
	Data        []byte
	ContentType string
}

// maxArtworkEntries bounds the in-memory poster cache; senders push a
// handful of items (now, next, schedule) so two dozen is generous.
const maxArtworkEntries = 24

// SetArtwork stores an image under the sender-chosen id.
func SetArtwork(id, contentType string, data []byte) {
	_metadataLock.Lock()
	defer _metadataLock.Unlock()
	if _, exists := _artwork[id]; !exists {
		_artworkOrder = append(_artworkOrder, id)
		if len(_artworkOrder) > maxArtworkEntries {
			delete(_artwork, _artworkOrder[0])
			_artworkOrder = _artworkOrder[1:]
		}
	}
	_artwork[id] = artworkEntry{Data: data, ContentType: contentType}
}

// GetArtwork returns a stored image, or nil.
func GetArtwork(id string) ([]byte, string) {
	_metadataLock.RLock()
	defer _metadataLock.RUnlock()
	entry, ok := _artwork[id]
	if !ok {
		return nil, ""
	}
	return entry.Data, entry.ContentType
}

// SetNowPlaying stores pushed metadata on the channel and optionally
// announces the change in chat.
func (c *ChannelRuntime) SetNowPlaying(np models.NowPlaying) {
	np.ReceivedAt = time.Now()

	_metadataLock.Lock()
	previous := c.nowPlaying
	c.nowPlaying = &np
	_metadataLock.Unlock()

	changed := previous == nil || previous.Title != np.Title || previous.Subtitle != np.Subtitle
	if np.Announce && changed && np.Title != "" && c.stats != nil && c.stats.StreamConnected {
		line := np.Title
		if np.Subtitle != "" {
			line = fmt.Sprintf("%s · %s", np.Title, np.Subtitle)
		}
		_ = chat.SendSystemAction(fmt.Sprintf("Now playing — **%s**", line), false)
	}
}

// GetNowPlaying returns the channel's current metadata, or nil.
func (c *ChannelRuntime) GetNowPlaying() *models.NowPlaying {
	_metadataLock.RLock()
	defer _metadataLock.RUnlock()
	return c.nowPlaying
}

// clearNowPlaying drops metadata when the stream ends.
func (c *ChannelRuntime) clearNowPlaying() {
	_metadataLock.Lock()
	c.nowPlaying = nil
	_metadataLock.Unlock()
}

// SetSchedule replaces the upcoming-showings list.
func SetSchedule(items []models.ScheduleItem) {
	_metadataLock.Lock()
	_schedule = items
	_metadataLock.Unlock()
}

// GetSchedule returns upcoming showings, soonest first, past ones dropped.
func GetSchedule() []models.ScheduleItem {
	_metadataLock.RLock()
	defer _metadataLock.RUnlock()
	upcoming := make([]models.ScheduleItem, 0, len(_schedule))
	for _, item := range _schedule {
		if item.StartsAt.After(time.Now().Add(-5 * time.Minute)) {
			upcoming = append(upcoming, item)
		}
	}
	return upcoming
}
