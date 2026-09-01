package transcoder

import (
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"streamingestarr/config"
	"streamingestarr/persistence/configrepository"
	"streamingestarr/utils"
)

// One generator per live channel, keyed by the channel's HLS directory —
// rooms broadcast concurrently and each needs its own thumbnail ticker.
var (
	_thumbGenMu sync.Mutex
	_thumbGens  = map[string]*time.Ticker{}
)

// ThumbnailPath returns where a channel's live thumbnail lives. The default
// channel keeps the legacy flat filename (the root page and social embeds
// reference it); other rooms get suffixed files beside it.
func ThumbnailPath(channelID string) string {
	if channelID == "" || channelID == "main" {
		return path.Join(config.TempDir, "thumbnail.jpg")
	}
	return path.Join(config.TempDir, "thumbnail-"+channelID+".jpg")
}

// PreviewGifPath returns where a channel's animated preview would live.
// Nothing generates these anymore — a still frame every generator tick
// replaced the looping gif (calmer on the room wall, far cheaper to make) —
// but the path keeps the legacy endpoint and cleanup honest.
func PreviewGifPath(channelID string) string {
	if channelID == "" || channelID == "main" {
		return path.Join(config.TempDir, "preview.gif")
	}
	return path.Join(config.TempDir, "preview-"+channelID+".gif")
}

// StopThumbnailGenerator stops the channel's periodic thumbnail generation.
func StopThumbnailGenerator(chunkPath string) {
	_thumbGenMu.Lock()
	defer _thumbGenMu.Unlock()
	if t, ok := _thumbGens[chunkPath]; ok {
		t.Stop()
		delete(_thumbGens, chunkPath)
	}
}

// StartThumbnailGenerator starts generating thumbnails for one channel's
// broadcast. channelID picks the output filenames so rooms never overwrite
// each other's preview.
func StartThumbnailGenerator(chunkPath string, variantIndex int, isVideoPassthrough bool, channelID string) {
	// Every 20 seconds create a thumbnail from the most
	// recent video segment.
	timer := time.NewTicker(20 * time.Second)

	_thumbGenMu.Lock()
	if old, ok := _thumbGens[chunkPath]; ok {
		old.Stop()
	}
	_thumbGens[chunkPath] = timer
	_thumbGenMu.Unlock()

	go func() {
		for range timer.C {
			if err := fireThumbnailGenerator(chunkPath, variantIndex, channelID); err != nil {
				logMsg := "Unable to generate thumbnail: " + err.Error()
				if isVideoPassthrough {
					logMsg += ". Video passthrough is enabled — the thumbnail has to decode whatever codec the sender pushes, which this ffmpeg may not support."
				}
				log.Errorln("Unable to generate thumbnail:", logMsg)
			}
		}
	}()
}

func fireThumbnailGenerator(segmentPath string, variantIndex int, channelID string) error {
	// JPG takes less time to encode than PNG
	outputFile := ThumbnailPath(channelID)

	framePath := path.Join(segmentPath, strconv.Itoa(variantIndex))
	files, err := os.ReadDir(framePath)
	if err != nil {
		return err
	}

	type seg struct {
		name string
		mod  time.Time
	}
	var segs []seg
	initSegment := ""
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "init-") && path.Ext(f.Name()) == ".mp4" {
			initSegment = f.Name()
			continue
		}
		if path.Ext(f.Name()) != ".ts" && path.Ext(f.Name()) != ".m4s" {
			continue
		}

		fi, err := f.Info()
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		segs = append(segs, seg{f.Name(), fi.ModTime()})
	}

	if len(segs) == 0 {
		return nil
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].mod.After(segs[j].mod) })

	configRepository := configrepository.Get()
	mostRecentFile := path.Join(framePath, segs[0].name)

	// An fMP4 media segment cannot be decoded on its own — prepend the
	// variant's init segment via ffmpeg's concat protocol.
	if path.Ext(mostRecentFile) == ".m4s" {
		if initSegment == "" {
			return nil
		}
		mostRecentFile = "concat:" + path.Join(framePath, initSegment) + "|" + mostRecentFile
	}
	ffmpegPath := utils.ValidatedFfmpegPath(configRepository.GetFfMpegPath())
	outputFileTemp := path.Join(config.TempDir, "tempthumbnail-"+channelID+".jpg")

	thumbnailCmdFlags := []string{
		"-y",            // Overwrite file
		"-threads", "1", // Low priority processing
		"-t", "1", // Pull from frame 1
		"-i", mostRecentFile, // Input
		"-f", "image2", // format
		"-vframes", "1", // Single frame
		outputFileTemp,
	}

	if _, err := exec.Command(ffmpegPath, thumbnailCmdFlags...).Output(); err != nil {
		return err
	}

	// rename temp file
	if err := utils.Move(outputFileTemp, outputFile); err != nil {
		log.Errorln(err)
	}

	return nil
}

