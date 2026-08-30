package storageproviders

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"
	"streamingestarr/persistence/configrepository"
)

// LocalStorage represents an instance of the local storage provider for HLS video.
type LocalStorage struct {
	host string
	// baseDir is the channel's HLS directory this provider manages.
	baseDir string
}

// NewLocalStorage returns a new LocalStorage instance rooted at baseDir.
func NewLocalStorage(baseDir string) *LocalStorage {
	return &LocalStorage{baseDir: baseDir}
}

// Setup configures this storage provider.
func (s *LocalStorage) Setup() error {
	configRepository := configrepository.Get()
	s.host = configRepository.GetVideoServingEndpoint()
	return nil
}

// SegmentWritten is called when a single segment of video is written.
func (s *LocalStorage) SegmentWritten(localFilePath string) {
	if _, err := s.Save(localFilePath, 0); err != nil {
		log.Warnln(err)
	}
}

// VariantPlaylistWritten is called when a variant hls playlist is written.
func (s *LocalStorage) VariantPlaylistWritten(localFilePath string) {
	if _, err := s.Save(localFilePath, 0); err != nil {
		log.Errorln(err)
		return
	}
}

// MasterPlaylistWritten is called when the master hls playlist is written.
func (s *LocalStorage) MasterPlaylistWritten(localFilePath string) {
	fixDegenerateMasterPlaylist(localFilePath)

	// If we're using a remote serving endpoint, we need to rewrite the master playlist
	if s.host != "" {
		if err := rewritePlaylistLocations(localFilePath, s.host, "", s.baseDir); err != nil {
			log.Warnln(err)
		}
	} else {
		if _, err := s.Save(localFilePath, 0); err != nil {
			log.Warnln(err)
		}
	}
}

// fixDegenerateMasterPlaylist repairs a master playlist that carries no
// variant entries. ffmpeg 9's hls muxer writes the master before the codec
// parameters of copied streams are known (observed with AV1) and only
// rewrites it when the stream ends — useless for live viewers. The CODECS
// attribute is optional in HLS (players derive codecs from the fMP4 init
// segment), so entries with just a bandwidth are valid and sufficient.
func fixDegenerateMasterPlaylist(localFilePath string) {
	contents, err := os.ReadFile(localFilePath) // nolint: gosec
	if err != nil || strings.Contains(string(contents), "#EXT-X-STREAM-INF") {
		return
	}

	configRepository := configrepository.Get()
	variants := configRepository.GetStreamOutputVariants()

	var b strings.Builder
	b.Write(contents)
	for index, variant := range variants {
		bandwidth := (variant.VideoBitrate + variant.AudioBitrate) * 1000
		if bandwidth == 0 {
			// Passthrough variants have no configured bitrate; a generous
			// estimate keeps players from refusing the entry.
			bandwidth = 6_000_000
		}
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d\n%d/stream.m3u8\n", bandwidth, index)
	}

	if err := os.WriteFile(localFilePath, []byte(b.String()), 0o644); err != nil { // nolint: gosec
		log.Warnln("unable to repair master playlist:", err)
		return
	}
	log.Traceln("repaired master playlist that was missing variant entries")
}

// Save will save a local filepath using the storage provider.
func (s *LocalStorage) Save(filePath string, retryCount int) (string, error) {
	return filePath, nil
}

// Cleanup will remove old files from the storage provider.
func (s *LocalStorage) Cleanup() error {
	// Determine how many files we should keep on disk
	configRepository := configrepository.Get()
	maxNumber := configRepository.GetStreamLatencyLevel().SegmentCount
	buffer := 10
	return localCleanup(s.baseDir, maxNumber+buffer)
}

func getAllFilesRecursive(baseDirectory string) (map[string][]os.FileInfo, error) {
	files := make(map[string][]os.FileInfo)

	var directory string
	err := filepath.Walk(baseDirectory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			directory = info.Name()
		}

		if filepath.Ext(info.Name()) == ".ts" {
			files[directory] = append(files[directory], info)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort by date so we can delete old files
	for directory := range files {
		sort.Slice(files[directory], func(i, j int) bool {
			return files[directory][i].ModTime().UnixNano() > files[directory][j].ModTime().UnixNano()
		})
	}

	return files, nil
}
