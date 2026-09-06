package core

import (
	"io"
	"streamingestarr/config"
	"time"

	log "github.com/sirupsen/logrus"

	"streamingestarr/core/chat"
	"streamingestarr/core/rtmp"
	"streamingestarr/core/srt"
	"streamingestarr/core/transcoder"
	"streamingestarr/core/webhooks"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
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
	lastDisconnect := c.stats.LastDisconnectTime
	c.stats.StreamConnected = true
	c.stats.LastDisconnectTime = nil
	c.stats.LastConnectTime = &now
	c.stats.SessionMaxViewerCount = 0
	// A session replacing one that just ended keeps its metadata — the
	// frame-size reconnect the grace exists for. Anything arriving later
	// (or after a receiver restart, where no drop timer survives but the
	// disk mirror restored the old now-playing) starts clean: the last
	// sender's metadata is not this broadcast's word.
	c.cancelNowPlayingDrop()
	if lastDisconnect == nil || !lastDisconnect.Valid || time.Since(lastDisconnect.Time) > nowPlayingGrace {
		c.dropNowPlaying()
	}

	configRepository := configrepository.Get()
	_ = configRepository

	c.currentBroadcast = &models.CurrentBroadcast{
		LatencyLevel:   channelrepository.GetEffectiveLatencyLevel(c.ID),
		OutputSettings: channelrepository.GetEffectiveOutputVariants(c.ID),
	}

	c.StopOfflineCleanupTimer()
	c.startOnlineCleanupTimer()

	if err := c.setupStorage(); err != nil {
		log.Fatalln("failed to setup the storage", err)
	}

	go func() {
		c.transcoder = transcoder.NewTranscoder(c.ID)
		c.transcoder.SetOutputPath(c.HLSOutputPath)
		c.transcoder.SetInternalHTTPPort(c.fileWriter.Port())
		// The relay feed rides every broadcast: a remux costs nothing, and
		// a room switched to relay mid-show has its links live at once.
		if url, video := c.relayBegin(); url != "" {
			c.transcoder.SetRelayOutput(url, video, config.GetInboundVideoRange(c.ID))
		}
		c.transcoder.TranscoderCompleted = func(error) {
			c.relayEnd()
			c.SetStreamAsDisconnected()
			c.transcoder = nil
			c.currentBroadcast = nil
		}
		c.transcoder.SetStdin(rtmpOut)
		c.transcoder.Start(true)
	}()

	go webhooks.SendStreamStatusEvent(models.StreamStarted, c.ID)
	selectedThumbnailVideoQualityIndex, isVideoPassthrough := configRepository.FindHighestVideoQualityIndex(c.currentBroadcast.OutputSettings)
	transcoder.StartThumbnailGenerator(c.HLSOutputPath, selectedThumbnailVideoQualityIndex, isVideoPassthrough, c.ID)

	_ = chat.SendSystemAction(c.ID, "Stay tuned, the stream is **starting**!", true)
	chat.SendAllWelcomeMessage(c.ID)
}

// SetStreamAsDisconnected sets the channel's stream as disconnected.
func (c *ChannelRuntime) SetStreamAsDisconnected() {
	_ = chat.SendSystemAction(c.ID, "The stream is ending.", true)

	now := utils.NullTime{Time: time.Now(), Valid: true}

	c.stats.StreamConnected = false
	c.stats.LastDisconnectTime = &now
	c.stats.LastConnectTime = nil
	c.broadcaster = nil
	c.rememberNowPlaying()
	c.deferNowPlayingDrop()
	// Votes belong to the broadcast that just ended.
	c.resetPauseVote()

	offlineFilename := "offline-v2.ts"

	offlineFilePath, err := saveOfflineClipToDisk(offlineFilename)
	if err != nil {
		log.Errorln(err)
		return
	}

	transcoder.StopThumbnailGenerator(c.HLSOutputPath)
	rtmp.Disconnect(c.ID)
	srt.Disconnect(c.ID)

	// If there is no current broadcast available the previous stream
	// likely failed for some reason. Don't try to append to it.
	// Just transition to offline.
	if c.currentBroadcast == nil {
		c.stopOnlineCleanupTimer()
		c.transitionToOfflineVideoStreamContent()
		log.Errorln("unexpected nil _currentBroadcast")
		return
	}

	if channelrepository.GetEffectiveSegmentFormat(c.ID) == "fmp4" {
		// The offline clip is an mpegts segment; appending it to an fMP4
		// playlist is invalid. Transition straight to the offline state
		// instead, which re-runs the transcoder in the configured format.
		c.stopOnlineCleanupTimer()
		c.transitionToOfflineVideoStreamContent()
		c.StartOfflineCleanupTimer()
		c.saveStats()
		go webhooks.SendStreamStatusEvent(models.StreamStopped, c.ID)
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

// nowPlayingGrace is how long a stream's metadata outlives its session.
// A sender that changes frame size opens a new session within seconds,
// and dropping the metadata at the disconnect wiped what its restate had
// just delivered — the viewer's ring fell back to a stopwatch until the
// next clip. Only a stream that stays gone loses its now-playing.
const nowPlayingGrace = 90 * time.Second

// deferNowPlayingDrop drops the now-playing once the grace has passed
// with no new session; a reconnect cancels it.
func (c *ChannelRuntime) deferNowPlayingDrop() {
	c.cancelNowPlayingDrop()
	c.nowPlayingDropTimer = time.AfterFunc(nowPlayingGrace, func() {
		if c.stats != nil && c.stats.StreamConnected {
			return
		}
		c.dropNowPlaying()
	})
}

// cancelNowPlayingDrop keeps the metadata: the stream is back.
func (c *ChannelRuntime) cancelNowPlayingDrop() {
	if c.nowPlayingDropTimer != nil {
		c.nowPlayingDropTimer.Stop()
		c.nowPlayingDropTimer = nil
	}
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
