package rtmp

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/nareix/joy5/format/flv/flvio"
	"streamingestarr/models"
)

const unknownString = "Unknown"

var _getInboundDetailsFromMetadataRE = regexp.MustCompile(`\{(.*?)\}`)

func getInboundDetailsFromMetadata(metadata []interface{}) (models.RTMPStreamMetadata, error) {
	metadataComponentsString := fmt.Sprintf("%+v", metadata)
	if !strings.Contains(metadataComponentsString, "onMetaData") {
		return models.RTMPStreamMetadata{}, errors.New("not an onMetaData message")
	}

	submatchall := _getInboundDetailsFromMetadataRE.FindAllString(metadataComponentsString, 1)

	if len(submatchall) == 0 {
		return models.RTMPStreamMetadata{}, errors.New("unable to parse inbound metadata")
	}

	metadataJSONString := submatchall[0]
	var details models.RTMPStreamMetadata
	err := json.Unmarshal([]byte(metadataJSONString), &details)
	return details, err
}

func getAudioCodec(codec interface{}) string {
	if codec == nil {
		return "No audio"
	}

	var codecID float64
	if assertedCodecID, ok := codec.(float64); ok {
		codecID = assertedCodecID
	} else {
		return codec.(string)
	}

	switch codecID {
	case flvio.SOUND_MP3:
		return "MP3"
	case flvio.SOUND_AAC:
		return "AAC"
	case flvio.SOUND_SPEEX:
		return "Speex"
	}

	return unknownString
}

func getVideoCodec(codec interface{}) string {
	if codec == nil {
		return unknownString
	}

	var codecID float64
	if assertedCodecID, ok := codec.(float64); ok {
		codecID = assertedCodecID
	} else {
		return codec.(string)
	}

	switch codecID {
	case flvio.VIDEO_H264:
		return "H.264"
	case flvio.VIDEO_H265:
		return "H.265"
	}

	return unknownString
}

func secretMatch(configStreamKey string, path string) bool {
	// Only the final path segment is the key. The application path before
	// it is whatever the encoder chose — "/live" (the RTMP convention and
	// what Owncast demanded), something else, or nothing at all. Demanding
	// a specific app name bought nothing; the key is the lock.
	path = strings.TrimSuffix(path, "/")
	idx := strings.LastIndex(path, "/")
	if idx < 0 || idx == len(path)-1 {
		return false
	}
	streamingKey := path[idx+1:]
	return subtle.ConstantTimeCompare([]byte(streamingKey), []byte(configStreamKey)) == 1
}
