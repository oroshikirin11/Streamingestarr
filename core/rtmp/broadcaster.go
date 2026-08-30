package rtmp

import (
	"time"

	"github.com/nareix/joy5/format/flv/flvio"
	log "github.com/sirupsen/logrus"
	"streamingestarr/models"
)

func setCurrentBroadcasterInfo(t flvio.Tag, remoteAddr string, streamKey string) {
	data, err := getInboundDetailsFromMetadata(t.DebugFields())
	if err != nil {
		log.Traceln("Unable to parse inbound broadcaster details:", err)
	}

	broadcaster := models.Broadcaster{
		RemoteAddr: remoteAddr,
		Time:       time.Now(),
		StreamDetails: models.InboundStreamDetails{
			Width:          data.Width,
			Height:         data.Height,
			VideoBitrate:   int(data.VideoBitrate),
			VideoCodec:     getVideoCodec(data.VideoCodec),
			VideoFramerate: data.VideoFramerate,
			AudioBitrate:   int(data.AudioBitrate),
			AudioCodec:     getAudioCodec(data.AudioCodec),
			Encoder:        data.Encoder,
			VideoOnly:      data.AudioCodec == nil,
		},
	}

	_setBroadcaster(broadcaster, streamKey)
}
