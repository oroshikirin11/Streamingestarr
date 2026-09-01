package core

import (
	"streamingestarr/config"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
)

// GetStatus gets the status of a channel.
func (c *ChannelRuntime) GetStatus() models.Status {
	if c.stats == nil {
		return models.Status{}
	}

	viewerCount := 0
	if c.IsStreamConnected() {
		viewerCount = len(c.stats.Viewers)
	}

	return models.Status{
		ChannelID:             c.ID,
		Online:                c.IsStreamConnected(),
		ViewerCount:           viewerCount,
		OverallMaxViewerCount: c.stats.OverallMaxViewerCount,
		SessionMaxViewerCount: c.stats.SessionMaxViewerCount,
		LastDisconnectTime:    c.stats.LastDisconnectTime,
		LastConnectTime:       c.stats.LastConnectTime,
		VersionNumber:         config.VersionNumber,
		StreamTitle:           channelrepository.GetEffectiveStreamTitle(c.ID),
	}
}

// GetStatus gets the status of the default channel.
func GetStatus() models.Status {
	c := Default()
	if c == nil {
		return models.Status{}
	}
	return c.GetStatus()
}

// getChannelStatus returns one channel's status by ID — the per-room lens
// the chat server sees the world through.
func getChannelStatus(channelID string) models.Status {
	c := GetChannelRuntime(channelID)
	if c == nil {
		return models.Status{}
	}
	return c.GetStatus()
}

// GetCurrentBroadcast will return the channel's currently active broadcast.
func (c *ChannelRuntime) GetCurrentBroadcast() *models.CurrentBroadcast {
	return c.currentBroadcast
}

// GetCurrentBroadcast will return the default channel's active broadcast.
func GetCurrentBroadcast() *models.CurrentBroadcast {
	return Default().GetCurrentBroadcast()
}

// setBroadcaster will store the current inbound broadcasting details on
// the channel the stream key feeds.
func setBroadcaster(broadcaster models.Broadcaster, streamKey string) {
	c := ChannelRuntimeForStreamKey(streamKey)
	c.broadcaster = &broadcaster
}

// GetBroadcaster will return the details of the channel's active broadcaster.
func (c *ChannelRuntime) GetBroadcaster() *models.Broadcaster {
	return c.broadcaster
}

// GetBroadcaster returns the default channel's active broadcaster.
func GetBroadcaster() *models.Broadcaster {
	return Default().GetBroadcaster()
}
