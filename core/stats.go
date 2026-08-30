package core

import (
	"math"
	"time"

	log "github.com/sirupsen/logrus"

	"streamingestarr/models"
	"streamingestarr/persistence/configrepository"
	"streamingestarr/services/geoip"
)

var (
	_activeViewerPurgeTimeout = time.Second * 15
	_geoIPClient              = geoip.NewClient()
)

func (c *ChannelRuntime) setupStats() error {
	s := getSavedStats()
	c.stats = &s

	statsSaveTimer := time.NewTicker(1 * time.Minute)
	go func() {
		for range statsSaveTimer.C {
			c.saveStats()
		}
	}()

	viewerCountPruneTimer := time.NewTicker(5 * time.Second)
	go func() {
		for range viewerCountPruneTimer.C {
			c.pruneViewerCount()
		}
	}()

	return nil
}

// IsStreamConnected checks if the channel's stream is connected or not.
func (c *ChannelRuntime) IsStreamConnected() bool {
	if !c.stats.StreamConnected {
		return false
	}

	// Kind of a hack.  It takes a handful of seconds between a RTMP connection and when HLS data is available.
	// So account for that with an artificial buffer of four segments.
	timeSinceLastConnected := time.Since(c.stats.LastConnectTime.Time).Seconds()
	configRepository := configrepository.Get()
	waitTime := math.Max(float64(configRepository.GetStreamLatencyLevel().SecondsPerSegment)*3.0, 7)
	if timeSinceLastConnected < waitTime {
		return false
	}

	return c.stats.StreamConnected
}

// IsStreamConnected reports the default channel's stream state.
func IsStreamConnected() bool {
	return Default().IsStreamConnected()
}

// RemoveChatClient removes a client from the active clients record.
func (c *ChannelRuntime) RemoveChatClient(clientID string) {
	log.Trace("Removing the client:", clientID)

	c.viewersLock.Lock()
	delete(c.stats.ChatClients, clientID)
	c.viewersLock.Unlock()
}

// RemoveChatClient removes a client from the default channel.
func RemoveChatClient(clientID string) {
	Default().RemoveChatClient(clientID)
}

// SetViewerActive sets a client as active and connected.
func (c *ChannelRuntime) SetViewerActive(viewer *models.Viewer) {
	// Don't update viewer counts if a live stream session is not active.
	if !c.stats.StreamConnected {
		return
	}

	c.viewersLock.Lock()
	defer c.viewersLock.Unlock()

	// Asynchronously, optionally, fetch GeoIP configRepository.
	go func(viewer *models.Viewer) {
		viewer.Geo = _geoIPClient.GetGeoFromIP(viewer.IPAddress)
	}(viewer)

	if _, exists := c.stats.Viewers[viewer.ClientID]; exists {
		c.stats.Viewers[viewer.ClientID].LastSeen = time.Now()
	} else {
		c.stats.Viewers[viewer.ClientID] = viewer
	}
	c.stats.SessionMaxViewerCount = int(math.Max(float64(len(c.stats.Viewers)), float64(c.stats.SessionMaxViewerCount)))
	c.stats.OverallMaxViewerCount = int(math.Max(float64(c.stats.SessionMaxViewerCount), float64(c.stats.OverallMaxViewerCount)))
}

// SetViewerActive marks a viewer on the default channel.
func SetViewerActive(viewer *models.Viewer) {
	Default().SetViewerActive(viewer)
}

// GetActiveViewers will return the channel's active viewers.
func (c *ChannelRuntime) GetActiveViewers() map[string]*models.Viewer {
	return c.stats.Viewers
}

// GetActiveViewers returns the default channel's active viewers.
func GetActiveViewers() map[string]*models.Viewer {
	return Default().GetActiveViewers()
}

func (c *ChannelRuntime) pruneViewerCount() {
	viewers := make(map[string]*models.Viewer)

	c.viewersLock.Lock()
	defer c.viewersLock.Unlock()

	for viewerID, viewer := range c.stats.Viewers {
		viewerLastSeenTime := c.stats.Viewers[viewerID].LastSeen
		if time.Since(viewerLastSeenTime) < _activeViewerPurgeTimeout {
			viewers[viewerID] = viewer
		}
	}

	c.stats.Viewers = viewers
}

// Peak counts persist through the config repository, which is not yet
// channel-scoped — with one channel that is exact; per-channel keys arrive
// with the second channel.
func (c *ChannelRuntime) saveStats() {
	configRepository := configrepository.Get()
	if err := configRepository.SetPeakOverallViewerCount(c.stats.OverallMaxViewerCount); err != nil {
		log.Errorln("error saving viewer count", err)
	}
	if err := configRepository.SetPeakSessionViewerCount(c.stats.SessionMaxViewerCount); err != nil {
		log.Errorln("error saving viewer count", err)
	}
	if c.stats.LastDisconnectTime != nil && c.stats.LastDisconnectTime.Valid {
		if err := configRepository.SetLastDisconnectTime(c.stats.LastDisconnectTime.Time); err != nil {
			log.Errorln("error saving disconnect time", err)
		}
	}
}

func getSavedStats() models.Stats {
	configRepository := configrepository.Get()
	savedLastDisconnectTime, _ := configRepository.GetLastDisconnectTime()

	result := models.Stats{
		ChatClients:           make(map[string]models.Client),
		Viewers:               make(map[string]*models.Viewer),
		SessionMaxViewerCount: configRepository.GetPeakSessionViewerCount(),
		OverallMaxViewerCount: configRepository.GetPeakOverallViewerCount(),
		LastDisconnectTime:    savedLastDisconnectTime,
	}

	// If the stats were saved > 5min ago then ignore the
	// peak session count value, since the session is over.
	if result.LastDisconnectTime == nil || !result.LastDisconnectTime.Valid || time.Since(result.LastDisconnectTime.Time).Minutes() > 5 {
		result.SessionMaxViewerCount = 0
	}

	return result
}
