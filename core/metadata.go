package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"streamingestarr/config"
	"streamingestarr/core/chat"
	"streamingestarr/models"
)

// Metadata is held in memory and mirrored to disk
// (data/metadata.json + data/artwork/) so a receiver restart does not
// blank the theater until the sender happens to push again.
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
	if _, exists := _artwork[id]; !exists {
		_artworkOrder = append(_artworkOrder, id)
		if len(_artworkOrder) > maxArtworkEntries {
			evicted := _artworkOrder[0]
			delete(_artwork, evicted)
			_artworkOrder = _artworkOrder[1:]
			_ = os.Remove(artworkFilePath(evicted))
		}
	}
	_artwork[id] = artworkEntry{Data: data, ContentType: contentType}
	_metadataLock.Unlock()

	_ = os.MkdirAll(filepath.Join(config.DataDirectory, "artwork"), 0o700)
	if err := os.WriteFile(artworkFilePath(id), data, 0o600); err != nil {
		log.Debugln("unable to persist artwork:", err)
	}
	persistMetadata()
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

	persistMetadata()

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
	persistMetadata()
}

// SetSchedule replaces the upcoming-showings list.
func SetSchedule(items []models.ScheduleItem) {
	_metadataLock.Lock()
	_schedule = items
	_metadataLock.Unlock()
	persistMetadata()
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

// ---------- persistence ----------

type persistedMetadata struct {
	Schedule   []models.ScheduleItem         `json:"schedule,omitempty"`
	NowPlaying map[string]*models.NowPlaying `json:"nowPlaying,omitempty"`
	Artwork    map[string]string             `json:"artwork,omitempty"` // id -> content type
}

func metadataStatePath() string {
	return filepath.Join(config.DataDirectory, "metadata.json")
}

func artworkFilePath(id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(config.DataDirectory, "artwork", hex.EncodeToString(sum[:16]))
}

// persistMetadata snapshots the in-memory state. Callers hold no locks.
func persistMetadata() {
	_metadataLock.RLock()
	state := persistedMetadata{
		Schedule:   _schedule,
		NowPlaying: map[string]*models.NowPlaying{},
		Artwork:    map[string]string{},
	}
	_channelsLock.RLock()
	for id, c := range _channels {
		if c.nowPlaying != nil {
			state.NowPlaying[id] = c.nowPlaying
		}
	}
	_channelsLock.RUnlock()
	for id, entry := range _artwork {
		state.Artwork[id] = entry.ContentType
	}
	_metadataLock.RUnlock()

	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	tmp := metadataStatePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		log.Debugln("unable to persist metadata:", err)
		return
	}
	_ = os.Rename(tmp, metadataStatePath())
}

// loadMetadata restores state at startup; called from core.Start after the
// channel runtimes exist.
func loadMetadata() {
	data, err := os.ReadFile(metadataStatePath())
	if err != nil {
		return
	}
	var state persistedMetadata
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}

	_metadataLock.Lock()
	_schedule = state.Schedule
	for id, contentType := range state.Artwork {
		bytes, err := os.ReadFile(artworkFilePath(id))
		if err != nil {
			continue
		}
		_artwork[id] = artworkEntry{Data: bytes, ContentType: contentType}
		_artworkOrder = append(_artworkOrder, id)
	}
	_metadataLock.Unlock()

	_channelsLock.RLock()
	for id, np := range state.NowPlaying {
		if c, ok := _channels[id]; ok {
			c.nowPlaying = np
		}
	}
	_channelsLock.RUnlock()
}
