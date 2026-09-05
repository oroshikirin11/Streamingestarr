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
	"streamingestarr/core/storageproviders"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
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

	onAir := c.stats != nil && c.stats.StreamConnected

	_metadataLock.Lock()
	previous := c.nowPlaying
	// The pause clock survives pushes that do not restate it; a pause the
	// push introduces starts now.
	if np.Paused && np.PausedAt == 0 {
		if previous != nil && previous.Paused && previous.PausedAt != 0 {
			np.PausedAt = previous.PausedAt
		} else {
			np.PausedAt = np.ReceivedAt.Unix()
		}
	} else if !np.Paused {
		np.PausedAt = 0
	}
	c.nowPlaying = &np
	// Only what this room actually aired can become its memory. A push
	// for a room with no stream connected is metadata about somewhere
	// else, and remembering it is how one room's film turned up in
	// another room's lobby.
	if onAir && np.Title != "" {
		c.lastOnAir = &models.LastPlayed{
			Title:    np.Title,
			Subtitle: np.Subtitle,
			EndedAt:  time.Now(),
		}
	}
	_metadataLock.Unlock()

	persistMetadata()

	// The push is the sender's word on paused/pending/controls too.
	c.applyPauseTransition()

	changed := previous == nil || previous.Title != np.Title || previous.Subtitle != np.Subtitle
	if np.Announce && changed && np.Title != "" && onAir {
		line := np.Title
		if np.Subtitle != "" {
			line = fmt.Sprintf("%s · %s", np.Title, np.Subtitle)
		}
		_ = chat.SendSystemAction(c.ID, fmt.Sprintf("Now playing — **%s**", line), false)
	}
}

// SetVideoRange records the colour range the sender declared for the
// channel's current broadcast (sdr|pq|hlg). When it changes and a stream is
// already live it re-signals the master playlist immediately, so HDR
// clients pick up VIDEO-RANGE without waiting for the next master write.
func (c *ChannelRuntime) SetVideoRange(v string) {
	previous := config.GetInboundVideoRange(c.ID)
	config.SetInboundVideoRange(c.ID, v)
	if config.GetInboundVideoRange(c.ID) == previous {
		return
	}
	masterPath := filepath.Join(c.HLSOutputPath, "stream.m3u8")
	if _, err := os.Stat(masterPath); err == nil {
		storageproviders.RepairMasterPlaylist(masterPath, c.ID)
	}
}

// GetNowPlaying returns the channel's current metadata, or nil.
func (c *ChannelRuntime) GetNowPlaying() *models.NowPlaying {
	_metadataLock.RLock()
	defer _metadataLock.RUnlock()
	return c.nowPlaying
}

// rememberNowPlaying keeps the title for the lobby's "last watched
// together" line. Called at the disconnect, while the memory is fresh; the
// metadata itself stays for a grace period, see deferNowPlayingDrop.
func (c *ChannelRuntime) rememberNowPlaying() {
	_metadataLock.Lock()
	// A sender that pushes once, a moment BEFORE its stream connects,
	// would otherwise leave no memory at all. Its push still belongs to
	// this broadcast when it arrived after the connection began — which
	// the receiver's own ReceivedAt stamp settles, and which a push for
	// a room that never connected can never satisfy.
	if c.lastOnAir == nil && c.nowPlaying != nil && c.nowPlaying.Title != "" &&
		c.stats != nil && c.stats.LastConnectTime != nil &&
		!c.nowPlaying.ReceivedAt.Before(c.stats.LastConnectTime.Time) {
		c.lastOnAir = &models.LastPlayed{
			Title:    c.nowPlaying.Title,
			Subtitle: c.nowPlaying.Subtitle,
			EndedAt:  time.Now(),
		}
	}
	// What aired HERE, not whatever the metadata slot happens to hold.
	if c.lastOnAir != nil && c.lastOnAir.Title != "" {
		c.lastPlayed = &models.LastPlayed{
			Title:    c.lastOnAir.Title,
			Subtitle: c.lastOnAir.Subtitle,
			EndedAt:  time.Now(),
		}
	}
	c.lastOnAir = nil
	_metadataLock.Unlock()
	persistMetadata()
}

// dropNowPlaying forgets the metadata: the stream stayed gone.
func (c *ChannelRuntime) dropNowPlaying() {
	_metadataLock.Lock()
	c.nowPlaying = nil
	_metadataLock.Unlock()
	// The colour range belongs to the broadcast that just ended; clear it so
	// a following SDR stream isn't mis-signaled as HDR before its first push.
	config.SetInboundVideoRange(c.ID, config.VideoRangeSDR)
	persistMetadata()
	c.resetPauseVote()
}

// GetLastPlayed returns what THIS room watched most recently, or nil —
// one room's ended movie must not haunt another room's lobby.
func (c *ChannelRuntime) GetLastPlayed() *models.LastPlayed {
	_metadataLock.RLock()
	defer _metadataLock.RUnlock()
	return c.lastPlayed
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
	// LastPlayed is the legacy single-room field; read once at load and
	// assigned to the default channel, never written anymore.
	LastPlayed          *models.LastPlayed            `json:"lastPlayed,omitempty"`
	LastPlayedByChannel map[string]*models.LastPlayed `json:"lastPlayedByChannel,omitempty"`
	Schedule            []models.ScheduleItem         `json:"schedule,omitempty"`
	NowPlaying          map[string]*models.NowPlaying `json:"nowPlaying,omitempty"`
	Artwork             map[string]string             `json:"artwork,omitempty"` // id -> content type
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
		LastPlayedByChannel: map[string]*models.LastPlayed{},
		Schedule:            _schedule,
		NowPlaying:          map[string]*models.NowPlaying{},
		Artwork:             map[string]string{},
	}
	_channelsLock.RLock()
	for id, c := range _channels {
		if c.nowPlaying != nil {
			state.NowPlaying[id] = c.nowPlaying
		}
		if c.lastPlayed != nil {
			state.LastPlayedByChannel[id] = c.lastPlayed
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
	for id, lp := range state.LastPlayedByChannel {
		if c, ok := _channels[id]; ok {
			c.lastPlayed = lp
		}
	}
	// Legacy single-room value: it belonged to the only room there was.
	if state.LastPlayed != nil {
		if c, ok := _channels[channelrepository.DefaultChannelID]; ok && c.lastPlayed == nil {
			c.lastPlayed = state.LastPlayed
		}
	}
	_channelsLock.RUnlock()
}
