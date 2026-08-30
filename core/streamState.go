package core

import (
	"io"
	"time"

	log "github.com/sirupsen/logrus"

	"streamingestarr/core/chat"
	"streamingestarr/core/rtmp"
	"streamingestarr/core/transcoder"
	"streamingestarr/core/webhooks"
	"streamingestarr/models"
	"streamingestarr/persistence/configrepository"
	"streamingestarr/utils"
)

// setStreamAsConnected resolves the channel the stream key feeds and marks
// its stream as connected.
func setStreamAsConnected(rtmpOut *io.PipeReader, streamKey string) {
	ChannelRuntimeForStreamKey(streamKey).setStreamAsConnected(rtmpOut)
}

// setStreamAsConnected sets the stream as connected.
func (c *ChannelRuntime) setStreamAsConnected(rtmpOut *io.PipeReader) {
	now := utils.NullTime{Time: time.Now(), Valid: true}
	c.stats.StreamConnected = true
	c.stats.LastDisconnectTime = nil
	c.stats.LastConnectTime = &now
	c.stats.SessionMaxViewerCount = 0

	configRepository := configrepository.Get()

	c.currentBroadcast = &models.CurrentBroadcast{
		LatencyLevel:   configRepository.GetStreamLatencyLevel(),
		OutputSettings: configRepository.GetStreamOutputVariants(),
	}

	c.StopOfflineCleanupTimer()
	c.startOnlineCleanupTimer()

	if err := c.setupStorage(); err != nil {
		log.Fatalln("failed to setup the storage", err)
	}

	go func() {
		c.transcoder = transcoder.NewTranscoder()
		c.transcoder.SetOutputPath(c.HLSOutputPath)
		c.transcoder.SetInternalHTTPPort(c.fileWriter.Port())
		c.transcoder.TranscoderCompleted = func(error) {
			c.SetStreamAsDisconnected()
			c.transcoder = nil
			c.currentBroadcast = nil
		}
		c.transcoder.SetStdin(rtmpOut)
		c.transcoder.Start(true)
	}()

	go webhooks.SendStreamStatusEvent(models.StreamStarted, c.ID)
	selectedThumbnailVideoQualityIndex, isVideoPassthrough := configRepository.FindHighestVideoQualityIndex(c.currentBroadcast.OutputSettings)
	transcoder.StartThumbnailGenerator(c.HLSOutputPath, selectedThumbnailVideoQualityIndex, isVideoPassthrough)

	_ = chat.SendSystemAction("Stay tuned, the stream is **starting**!", true)
	chat.SendAllWelcomeMessage()
}

// SetStreamAsDisconnected sets the channel's stream as disconnected.
func (c *ChannelRuntime) SetStreamAsDisconnected() {
	_ = chat.SendSystemAction("The stream is ending.", true)

	now := utils.NullTime{Time: time.Now(), Valid: true}

	c.stats.StreamConnected = false
	c.stats.LastDisconnectTime = &now
	c.stats.LastConnectTime = nil
	c.broadcaster = nil

	offlineFilename := "offline-v2.ts"

	offlineFilePath, err := saveOfflineClipToDisk(offlineFilename)
	if err != nil {
		log.Errorln(err)
		return
	}

	transcoder.StopThumbnailGenerator()
	rtmp.Disconnect()

	// If there is no current broadcast available the previous stream
	// likely failed for some reason. Don't try to append to it.
	// Just transition to offline.
	if c.currentBroadcast == nil {
		c.stopOnlineCleanupTimer()
		c.transitionToOfflineVideoStreamContent()
		log.Errorln("unexpected nil _currentBroadcast")
		return
	}

	for index := range c.currentBroadcast.OutputSettings {
		c.makeVariantIndexOffline(index, offlineFilePath, offlineFilename)
	}

	c.StartOfflineCleanupTimer()
	c.stopOnlineCleanupTimer()
	c.saveStats()

	go webhooks.SendStreamStatusEvent(models.StreamStopped, c.ID)
}

// StartOfflineCleanupTimer will fire a cleanup after n minutes being disconnected.
func (c *ChannelRuntime) StartOfflineCleanupTimer() {
	c.offlineCleanupTimer = time.NewTimer(5 * time.Minute)
	go func() {
		for range c.offlineCleanupTimer.C {
			// Set video to offline state
			c.resetDirectories()
			c.transitionToOfflineVideoStreamContent()
		}
	}()
}

// StopOfflineCleanupTimer will stop the previous cleanup timer.
func (c *ChannelRuntime) StopOfflineCleanupTimer() {
	if c.offlineCleanupTimer != nil {
		c.offlineCleanupTimer.Stop()
	}
}

func (c *ChannelRuntime) startOnlineCleanupTimer() {
	c.onlineCleanupTicker = time.NewTicker(1 * time.Minute)
	go func() {
		for range c.onlineCleanupTicker.C {
			if err := c.storage.Cleanup(); err != nil {
				log.Errorln(err)
			}
		}
	}()
}

func (c *ChannelRuntime) stopOnlineCleanupTimer() {
	if c.onlineCleanupTicker != nil {
		c.onlineCleanupTicker.Stop()
	}
}

// SetStreamAsDisconnected sets the default channel's stream as disconnected.
func SetStreamAsDisconnected() {
	Default().SetStreamAsDisconnected()
}
