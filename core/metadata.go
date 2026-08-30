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
)

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
