package storageproviders

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"
	"streamingestarr/config"
	"streamingestarr/core/avsync"
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
	// One parse-only probe per finished segment: the A/V offset ledger
	// that tells sender seams apart from player drift.
	avsync.MeasureSegment(localFilePath)
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

// RepairMasterPlaylist re-applies the master-playlist fixes below to an
// already-written master. core calls it when the inbound video range changes
// mid-broadcast, after ffmpeg has already written the master once.
func RepairMasterPlaylist(localFilePath string) {
	fixDegenerateMasterPlaylist(localFilePath)
}

// fixDegenerateMasterPlaylist repairs a master playlist that carries no
// variant entries and, for HDR broadcasts, ensures every variant advertises
// VIDEO-RANGE. ffmpeg 9's hls muxer writes the master before the codec
// parameters of copied streams are known (observed with AV1) and only
// rewrites it when the stream ends — useless for live viewers. The CODECS
// attribute is optional in HLS (players derive codecs from the fMP4 init
// segment), so entries with just a bandwidth are valid and sufficient.
func fixDegenerateMasterPlaylist(localFilePath string) {
	contents, err := os.ReadFile(localFilePath) // nolint: gosec
	if err != nil {
		return
	}
	text := string(contents)
	videoRange := config.HLSVideoRangeToken()

	// ffmpeg wrote a real master. Nothing to synthesize, but two repairs may
	// still apply: an HDR broadcast needs VIDEO-RANGE on each variant or
	// HDR-capable players treat it as SDR, and a passthrough variant's
	// CODECS may be a fabrication — ffmpeg 9's hls muxer stamps a default
	// avc1 string on copied streams whose parameters it has not seen yet,
	// which makes hls.js init an H.264 decoder against AV1/HEVC segments:
	// sound plays, picture never appears. When the claim contradicts the
	// codec the ingest sniffed from the actual stream, the attribute is
	// dropped and players derive the truth from the fMP4 init segment.
	if strings.Contains(text, "#EXT-X-STREAM-INF") {
		patched := stripLyingCodecs(text)
		if videoRange != "" && !strings.Contains(patched, "VIDEO-RANGE=") {
			patched = injectVideoRange(patched, videoRange)
		}
		if patched != text {
			if err := os.WriteFile(localFilePath, []byte(patched), 0o644); err != nil { // nolint: gosec
				log.Warnln("unable to repair master playlist attributes:", err)
			}
		}
		return
	}

	configRepository := configrepository.Get()
	variants := configRepository.GetStreamOutputVariants()

	var b strings.Builder
	b.WriteString(text)
	for index, variant := range variants {
		bandwidth := (variant.VideoBitrate + variant.AudioBitrate) * 1000
		if bandwidth == 0 {
			// Passthrough variants have no configured bitrate; a generous
			// estimate keeps players from refusing the entry.
			bandwidth = 6_000_000
		}
		attrs := fmt.Sprintf("BANDWIDTH=%d", bandwidth)
		if videoRange != "" {
			attrs += ",VIDEO-RANGE=" + videoRange
		}
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:%s\n%d/stream.m3u8\n", attrs, index)
	}

	if err := os.WriteFile(localFilePath, []byte(b.String()), 0o644); err != nil { // nolint: gosec
		log.Warnln("unable to repair master playlist:", err)
		return
	}
	log.Traceln("repaired master playlist that was missing variant entries")
}

// hlsCodecPrefixes maps ffprobe's codec name onto the RFC 6381 prefixes a
// truthful CODECS attribute would start its video entry with.
var hlsCodecPrefixes = map[string][]string{
	"h264": {"avc1", "avc3"},
	"hevc": {"hvc1", "hev1"},
	"av1":  {"av01"},
	"vp9":  {"vp09"},
}

var codecsAttr = regexp.MustCompile(`,?CODECS="[^"]*"`)

// stripLyingCodecs removes the CODECS attribute from EXT-X-STREAM-INF lines
// whose video codec claim contradicts what the ingest sniffed from the
// stream itself. Only variants that copy the video can be lied about — a
// transcoded variant really is the H.264 ffmpeg claims — so the check is
// per-variant, in the order ffmpeg writes them, honouring the HDR guard
// that forces passthrough regardless of variant config.
func stripLyingCodecs(text string) string {
	inbound := config.GetInboundVideoCodec()
	prefixes := hlsCodecPrefixes[inbound]
	if len(prefixes) == 0 {
		return text // unknown inbound codec: nothing to compare against
	}
	hdrForced := config.GetInboundVideoRange() != config.VideoRangeSDR
	variants := configrepository.Get().GetStreamOutputVariants()

	lines := strings.Split(text, "\n")
	variantIdx := 0
	for i, line := range lines {
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			continue
		}
		idx := variantIdx
		variantIdx++
		copied := hdrForced || (idx < len(variants) && variants[idx].IsVideoPassthrough)
		m := codecsAttr.FindString(line)
		if !copied || m == "" {
			continue
		}
		honest := false
		for _, p := range prefixes {
			if strings.Contains(m, p) {
				honest = true
				break
			}
		}
		if !honest {
			lines[i] = strings.Replace(line, m, "", 1)
		}
	}
	return strings.Join(lines, "\n")
}

// injectVideoRange appends VIDEO-RANGE=<token> to every EXT-X-STREAM-INF line
// that does not already carry it.
func injectVideoRange(playlist, token string) string {
	lines := strings.Split(playlist, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") && !strings.Contains(line, "VIDEO-RANGE=") {
			lines[i] = line + ",VIDEO-RANGE=" + token
		}
	}
	return strings.Join(lines, "\n")
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
