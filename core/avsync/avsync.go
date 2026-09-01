// Package avsync measures where audio/video alignment enters or leaves the
// stream. Every finished HLS segment gets one cheap ffprobe (parse-only,
// lowest CPU priority): the first audio PTS minus the first video PTS. If
// the segments on disk are aligned but playback drifts, the player is
// guilty; if the segments themselves skew after a stall or seam, the
// sender's recovery glue is. Without this number the two are
// indistinguishable from the couch.
//
// State is per channel — segment paths live under data/hls/<channel>/, and
// the ledger of one room must never describe another's broadcast.
package avsync

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"streamingestarr/config"
	"streamingestarr/persistence/channelrepository"
)

// Measurement is one segment's A/V offset plus its place in the two
// timelines that must agree for playback to feel smooth:
//
//   - DeltaMs: audio minus video first-PTS inside the segment (positive =
//     audio starts later). Skew here = the sender's A/V glue.
//   - PtsStepMs: how far the MEDIA advanced since the previous measured
//     segment (first video PTS delta). Wobble here — negative, or jumping
//     around the segment duration — is a timeline seam in the source.
//   - WallStepMs: how much WALL time passed between those segments being
//     written. Media steps steady while wall steps stretch = the feed is
//     arriving late (sender starving or the uplink); that is the
//     rubberband signature viewers feel as stall-then-jump.
//
// StepSegments is how many segments the step spans (probes are skipped
// while one is running) — divide the steps by it to normalize.
type Measurement struct {
	Time         time.Time `json:"time"`
	Segment      string    `json:"segment"`
	VideoMs      float64   `json:"videoMs"`
	AudioMs      float64   `json:"audioMs"`
	DeltaMs      float64   `json:"deltaMs"`
	PtsStepMs    float64   `json:"ptsStepMs"`
	WallStepMs   float64   `json:"wallStepMs"`
	StepSegments int       `json:"stepSegments"`
}

const keep = 900 // ~an hour of segments — the ledger must outlive the incident

// ledger is one channel's measurement state.
type ledger struct {
	mu      sync.Mutex
	ring    []Measurement
	busy    atomic.Bool
	initSeg atomic.Value // string: the session's fMP4 init file, "" for ts

	// The previous measured segment, for the step fields.
	lastVideoMs float64
	lastWall    time.Time
	lastSeq     int
	haveLast    bool
}

var (
	_ledgersMu sync.Mutex
	_ledgers   = map[string]*ledger{}
)

func ledgerFor(channelID string) *ledger {
	_ledgersMu.Lock()
	defer _ledgersMu.Unlock()
	l, ok := _ledgers[channelID]
	if !ok {
		l = &ledger{}
		l.initSeg.Store("")
		_ledgers[channelID] = l
	}
	return l
}

// channelFromPath derives which channel a segment path belongs to — the
// first path component under the HLS root (data/hls/<channel>/...).
func channelFromPath(path string) string {
	rel, err := filepath.Rel(config.HLSStoragePath, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return channelrepository.DefaultChannelID
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return channelrepository.DefaultChannelID
	}
	return parts[0]
}

// MeasureSegment probes one finished segment asynchronously. Non-reentrant
// per channel by design: if the channel's previous probe is still running
// the measurement is skipped, never queued — this is instrumentation, not
// bookkeeping.
func MeasureSegment(path string) {
	base := filepath.Base(path)
	l := ledgerFor(channelFromPath(path))
	switch filepath.Ext(base) {
	case ".ts":
		// fine as-is
	case ".m4s":
		// needs the init segment prepended
	default:
		if filepath.Ext(base) == ".mp4" {
			// The init segment itself — remember it, don't measure it.
			l.initSeg.Store(path)
		}
		return
	}
	if !l.busy.CompareAndSwap(false, true) {
		return
	}
	seq := segmentSeq(base)
	written := time.Now()
	go func() {
		defer l.busy.Store(false)
		if m, ok := l.probe(path, base); ok {
			l.mu.Lock()
			if l.haveLast {
				m.PtsStepMs = m.VideoMs - l.lastVideoMs
				m.WallStepMs = float64(written.Sub(l.lastWall).Milliseconds())
				m.StepSegments = 1
				if seq > 0 && l.lastSeq > 0 && seq > l.lastSeq {
					m.StepSegments = seq - l.lastSeq
				}
			}
			l.lastVideoMs = m.VideoMs
			l.lastWall = written
			l.lastSeq = seq
			l.haveLast = true
			l.ring = append(l.ring, m)
			if len(l.ring) > keep {
				l.ring = l.ring[len(l.ring)-keep:]
			}
			l.mu.Unlock()
		}
	}()
}

// Get returns the channel's recent measurements, oldest first.
func Get(channelID string) []Measurement {
	l := ledgerFor(channelID)
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Measurement, len(l.ring))
	copy(out, l.ring)
	return out
}

// Reset clears a channel's ledger — called when a new broadcaster session
// begins so the numbers always describe the current broadcast.
func Reset(channelID string) {
	l := ledgerFor(channelID)
	l.mu.Lock()
	l.ring = nil
	l.haveLast = false
	l.mu.Unlock()
	l.initSeg.Store("")
}

// segmentSeq parses the trailing sequence number ffmpeg stamps on segment
// filenames (stream-XXXX-N.ts / .m4s), or 0 when the shape is unfamiliar.
var segSeqRE = regexp.MustCompile(`-(\d+)\.(?:ts|m4s)$`)

func segmentSeq(base string) int {
	m := segSeqRE.FindStringSubmatch(base)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

type ffprobePacket struct {
	CodecType string `json:"codec_type"`
	PtsTime   string `json:"pts_time"`
	DtsTime   string `json:"dts_time"`
}

func (l *ledger) probe(path, base string) (Measurement, bool) {
	input := path
	if filepath.Ext(base) == ".m4s" {
		init, _ := l.initSeg.Load().(string)
		if init == "" {
			return Measurement{}, false
		}
		input = "concat:" + init + "|" + path
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	args := []string{
		"-v", "quiet", "-print_format", "json",
		// The first second of packets is plenty to find both first PTS.
		"-read_intervals", "%+1",
		"-show_entries", "packet=codec_type,pts_time,dts_time",
		"-i", input,
	}
	var cmd *exec.Cmd
	if nice, err := exec.LookPath("nice"); err == nil {
		cmd = exec.CommandContext(ctx, nice, append([]string{"-n", "19", "ffprobe"}, args...)...)
	} else {
		cmd = exec.CommandContext(ctx, "ffprobe", args...)
	}
	out, err := cmd.Output()
	if err != nil {
		log.Debugln("avsync probe failed for", base, err)
		return Measurement{}, false
	}
	var parsed struct {
		Packets []ffprobePacket `json:"packets"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &parsed); err != nil {
		return Measurement{}, false
	}
	video, audio := -1.0, -1.0
	for _, p := range parsed.Packets {
		t, err := strconv.ParseFloat(p.PtsTime, 64)
		if err != nil {
			if t, err = strconv.ParseFloat(p.DtsTime, 64); err != nil {
				continue
			}
		}
		switch p.CodecType {
		case "video":
			if video < 0 {
				video = t
			}
		case "audio":
			if audio < 0 {
				audio = t
			}
		}
		if video >= 0 && audio >= 0 {
			break
		}
	}
	if video < 0 || audio < 0 {
		return Measurement{}, false
	}
	return Measurement{
		Time:    time.Now(),
		Segment: base,
		VideoMs: video * 1000,
		AudioMs: audio * 1000,
		DeltaMs: (audio - video) * 1000,
	}, true
}
