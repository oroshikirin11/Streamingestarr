package srt

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"streamingestarr/models"
	"streamingestarr/persistence/configrepository"
	"streamingestarr/utils"
)

// RTMP hands us its metadata in the connect handshake; SRT hands us nothing
// but mpegts bytes. So the details the admin shows for an SRT broadcaster
// come from ffprobe over a captured prefix of the stream itself — which also
// makes them honest: this is what is actually in the container, not what the
// encoder claims about it.

type ffprobeStream struct {
	CodecType     string `json:"codec_type"`
	CodecName     string `json:"codec_name"`
	Profile       string `json:"profile"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	PixFmt        string `json:"pix_fmt"`
	ColorTransfer string `json:"color_transfer"`
	AvgFrameRate  string `json:"avg_frame_rate"`
	RFrameRate    string `json:"r_frame_rate"`
	BitRate       string `json:"bit_rate"`
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
}

var videoCodecNames = map[string]string{
	"h264": "H.264", "hevc": "HEVC", "av1": "AV1", "vp9": "VP9", "mpeg2video": "MPEG-2",
}

var audioCodecNames = map[string]string{
	"aac": "AAC", "ac3": "AC-3", "eac3": "E-AC-3", "opus": "Opus", "mp3": "MP3", "flac": "FLAC",
}

// probeInboundStream describes the captured mpegts prefix. A false return
// means the probe could not say anything useful; the caller keeps the
// bare "SRT/mpegts" details rather than showing half-parsed junk.
func probeInboundStream(prefix []byte) (models.InboundStreamDetails, bool) {
	details := models.InboundStreamDetails{Encoder: "SRT/mpegts"}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffprobePath(),
		"-v", "quiet", "-print_format", "json", "-show_streams",
		"-f", "mpegts", "-i", "pipe:0")
	cmd.Stdin = bytes.NewReader(prefix)
	out, err := cmd.Output()
	if err != nil {
		log.Debugln("SRT inbound stream probe failed:", err)
		return details, false
	}

	var parsed ffprobeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		log.Debugln("SRT inbound stream probe unreadable:", err)
		return details, false
	}

	sawVideo, sawAudio := false, false
	for _, s := range parsed.Streams {
		switch s.CodecType {
		case "video":
			if sawVideo {
				continue
			}
			sawVideo = true
			details.VideoCodec = describeVideo(s)
			details.Width = s.Width
			details.Height = s.Height
			details.VideoFramerate = parseFrameRate(s.AvgFrameRate, s.RFrameRate)
			details.VideoBitrate = parseKbps(s.BitRate)
		case "audio":
			if sawAudio {
				continue
			}
			sawAudio = true
			details.AudioCodec = describeAudio(s)
			details.AudioBitrate = parseKbps(s.BitRate)
		}
	}
	details.VideoOnly = sawVideo && !sawAudio
	return details, sawVideo || sawAudio
}

// describeVideo folds codec, profile and dynamic range into the one codec
// string the admin already renders — "HEVC Main 10 · HDR (PQ)" tells the
// operator what arrived without any new UI.
func describeVideo(s ffprobeStream) string {
	name := videoCodecNames[s.CodecName]
	if name == "" {
		name = strings.ToUpper(s.CodecName)
	}
	if p := s.Profile; p != "" && !strings.EqualFold(p, "unknown") {
		name += " " + p
	}
	switch s.ColorTransfer {
	case "smpte2084":
		name += " · HDR (PQ)"
	case "arib-std-b67":
		name += " · HDR (HLG)"
	default:
		// A 10-bit surface without an HDR transfer is still worth a word;
		// profiles like "Main 10" already say it, so only add when absent.
		if strings.Contains(s.PixFmt, "10") && !strings.Contains(name, "10") {
			name += " · 10-bit"
		}
	}
	return name
}

func describeAudio(s ffprobeStream) string {
	if name := audioCodecNames[s.CodecName]; name != "" {
		return name
	}
	return strings.ToUpper(s.CodecName)
}

// parseFrameRate turns ffprobe's "24000/1001" into 23.98. avg_frame_rate is
// preferred; r_frame_rate covers streams where the average is still "0/0"
// this early in the capture.
func parseFrameRate(rates ...string) float32 {
	for _, rate := range rates {
		num, den, ok := strings.Cut(rate, "/")
		if !ok {
			continue
		}
		n, errN := strconv.ParseFloat(num, 64)
		d, errD := strconv.ParseFloat(den, 64)
		if errN != nil || errD != nil || n <= 0 || d <= 0 {
			continue
		}
		return float32(math.Round(n/d*100) / 100)
	}
	return 0
}

// parseKbps converts ffprobe's bits-per-second string to the kbps int the
// admin renders. Per-stream rates are often absent in a bare TS prefix;
// zero simply leaves the field off the page.
func parseKbps(bps string) int {
	n, err := strconv.Atoi(bps)
	if err != nil || n <= 0 {
		return 0
	}
	return n / 1000
}

// ffprobePath looks next to the configured ffmpeg first — wherever the
// operator's ffmpeg lives, its ffprobe lives too — then falls back to PATH.
func ffprobePath() string {
	ffmpeg := utils.ValidatedFfmpegPath(configrepository.Get().GetFfMpegPath())
	candidate := filepath.Join(filepath.Dir(ffmpeg), "ffprobe")
	if _, err := exec.LookPath(candidate); err == nil {
		return candidate
	}
	return "ffprobe"
}
