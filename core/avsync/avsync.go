// Package avsync measures where audio/video alignment enters or leaves the
// stream. Every finished HLS segment gets one cheap ffprobe (parse-only,
// lowest CPU priority): the first audio PTS minus the first video PTS. If
// the segments on disk are aligned but playback drifts, the player is
// guilty; if the segments themselves skew after a stall or seam, the
// sender's recovery glue is. Without this number the two are
// indistinguishable from the couch.
package avsync

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

// Measurement is one segment's A/V offset. Delta is audio minus video in
// milliseconds: positive = audio starts later than video.
type Measurement struct {
	Time    time.Time `json:"time"`
	Segment string    `json:"segment"`
	VideoMs float64   `json:"videoMs"`
	AudioMs float64   `json:"audioMs"`
	DeltaMs float64   `json:"deltaMs"`
}

const keep = 90 // ~a few minutes of segments

var (
	_mu      sync.Mutex
	_ring    []Measurement
	_busy    atomic.Bool
	_initSeg atomic.Value // string: the session's fMP4 init file, "" for ts
)

// NoteInitSegment records the current session's fMP4 init file — a bare
// .m4s cannot be parsed without it; concatenated they form a valid
// fragmented MP4.
func NoteInitSegment(path string) { _initSeg.Store(path) }

// MeasureSegment probes one finished segment asynchronously. Non-reentrant
// by design: if the previous probe is still running the measurement is
// skipped, never queued — this is instrumentation, not bookkeeping.
func MeasureSegment(path string) {
	base := filepath.Base(path)
	switch filepath.Ext(base) {
	case ".ts":
		// fine as-is
	case ".m4s":
		// needs the init segment prepended
	default:
		if filepath.Ext(base) == ".mp4" {
			// The init segment itself — remember it, don't measure it.
			NoteInitSegment(path)
		}
		return
	}
	if !_busy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer _busy.Store(false)
		if m, ok := probe(path, base); ok {
			_mu.Lock()
			_ring = append(_ring, m)
			if len(_ring) > keep {
				_ring = _ring[len(_ring)-keep:]
			}
			_mu.Unlock()
		}
	}()
}

// Get returns the recent measurements, oldest first.
func Get() []Measurement {
	_mu.Lock()
	defer _mu.Unlock()
	out := make([]Measurement, len(_ring))
	copy(out, _ring)
	return out
}

// Reset clears the ring — called when a new broadcaster session begins so
// the numbers always describe the current broadcast.
func Reset() {
	_mu.Lock()
	_ring = nil
	_mu.Unlock()
	_initSeg.Store("")
}

type ffprobePacket struct {
	CodecType string `json:"codec_type"`
	PtsTime   string `json:"pts_time"`
	DtsTime   string `json:"dts_time"`
}

func probe(path, base string) (Measurement, bool) {
	input := path
	if filepath.Ext(base) == ".m4s" {
		init, _ := _initSeg.Load().(string)
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
