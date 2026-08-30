package core

import (
	"os"
	"path"
	"path/filepath"

	log "github.com/sirupsen/logrus"

	"streamingestarr/auth"
	"streamingestarr/config"
	"streamingestarr/core/chat"
	"streamingestarr/core/data"
	"streamingestarr/core/rtmp"
	"streamingestarr/core/srt"
	"streamingestarr/core/transcoder"
	"streamingestarr/core/webhooks"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/persistence/configrepository"
	"streamingestarr/persistence/tables"
	"streamingestarr/utils"
)

// Start starts up the core processing.
func Start() error {
	configRepository := configrepository.Get()

	if err := configRepository.VerifySettings(); err != nil {
		log.Error(err)
		return err
	}

	tables.SetupUsers(data.GetDatastore().DB)
	auth.Setup(data.GetDatastore().DB)
	channelrepository.Setup(data.GetDatastore().DB)

	// Wipe the HLS root once; each channel recreates its own directory.
	utils.CleanupDirectory(config.HLSStoragePath)

	// One runtime per channel. Exactly one exists today; this loop is the
	// multi-channel seam.
	for _, channel := range channelrepository.ListChannels() {
		c := newChannelRuntime(channel.ID)
		_channelsLock.Lock()
		_channels[channel.ID] = c
		_channelsLock.Unlock()

		if err := c.start(); err != nil {
			return err
		}
	}

	loadMetadata()

	// Chat is a single global room for now; it becomes per-channel when a
	// second theater actually exists (docs/design.md §8).
	if err := chat.Start(GetStatus); err != nil {
		log.Errorln(err)
	}

	// start the rtmp server
	go rtmp.Start(setStreamAsConnected, setBroadcaster, isStreamKeyBusy)

	rtmpPort := configRepository.GetRTMPPortNumber()
	if rtmpPort != 1935 {
		log.Infof("RTMP is accepting inbound streams on port %d.", rtmpPort)
	}

	// start the SRT/mpegts server — the preferred ingest path
	go srt.Start(setStreamAsConnected, setBroadcaster, isStreamKeyBusy)
	if configRepository.GetSRTServerEnabled() {
		log.Infof("SRT is accepting inbound streams on udp port %d.", configRepository.GetSRTServerPort())
	}

	webhooks.SetupWebhooks(GetStatus)

	return nil
}

// start brings one channel's runtime up.
func (c *ChannelRuntime) start() error {
	c.resetDirectories()

	if err := c.setupStats(); err != nil {
		log.Error("failed to setup the stats")
		return err
	}

	// The HLS handler takes the written HLS playlists and segments
	// and makes storage decisions.  It's rather simple right now
	// but will play more useful when recordings come into play.
	c.handler = transcoder.HLSHandler{}

	if err := c.setupStorage(); err != nil {
		log.Errorln("storage error", err)
	}

	c.fileWriter.SetupFileWriterReceiverService(&c.handler, c.HLSOutputPath)

	if err := c.createInitialOfflineState(); err != nil {
		log.Error("failed to create the initial offline state")
		return err
	}

	return nil
}

func (c *ChannelRuntime) createInitialOfflineState() error {
	c.transitionToOfflineVideoStreamContent()

	return nil
}

// transitionToOfflineVideoStreamContent will overwrite the current stream with the
// offline video stream state only.  No live stream HLS segments will continue to be
// referenced.
func (c *ChannelRuntime) transitionToOfflineVideoStreamContent() {
	log.Traceln("Firing transcoder with offline stream state")

	_transcoder := transcoder.NewTranscoder()
	_transcoder.SetIdentifier("offline")
	_transcoder.SetLatencyLevel(models.GetLatencyLevel(4))
	_transcoder.SetIsEvent(true)
	_transcoder.SetOutputPath(c.HLSOutputPath)
	_transcoder.SetInternalHTTPPort(c.fileWriter.Port())

	offlineFilePath, err := saveOfflineClipToDisk("offline-v2.ts")
	if err != nil {
		log.Fatalln("unable to save offline clip:", err)
	}

	_transcoder.SetInput(offlineFilePath)
	go _transcoder.Start(false)

	// Copy the logo to be the thumbnail
	configRepository := configrepository.Get()
	logo := configRepository.GetLogoPath()
	dst := filepath.Join(config.TempDir, "thumbnail.jpg")
	if err = utils.Copy(filepath.Join("data", logo), dst); err != nil {
		log.Warnln(err)
	}

	// Delete the preview Gif
	_ = os.Remove(path.Join(config.DataDirectory, "preview.gif"))
}

func (c *ChannelRuntime) resetDirectories() {
	log.Trace("Resetting file directories to a clean slate.")

	// Wipe this channel's hls data directory
	utils.CleanupDirectory(c.HLSOutputPath)

	// Remove the previous thumbnail
	configRepository := configrepository.Get()
	logo := configRepository.GetLogoPath()
	if utils.DoesFileExists(logo) {
		err := utils.Copy(path.Join("data", logo), filepath.Join(config.DataDirectory, "thumbnail.jpg"))
		if err != nil {
			log.Warnln(err)
		}
	}
}
