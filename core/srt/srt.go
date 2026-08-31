// Package srt implements the SRT/mpegts ingest — the preferred inbound
// path (docs/design.md §2): mpegts carries AV1 and HEVC natively, so the
// Enhanced-RTMP tagging problem never exists here.
package srt

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gosrt "github.com/datarhei/gosrt"
	log "github.com/sirupsen/logrus"

	"streamingestarr/config"
	"streamingestarr/models"
	"streamingestarr/persistence/configrepository"
)

var (
	_setStreamAsConnected func(*io.PipeReader, string)
	_setBroadcaster       func(models.Broadcaster, string)
	_isStreamBusy         func(string) bool

	_mu         sync.Mutex
	_activeConn gosrt.Conn
	_activePipe *io.PipeWriter
)

// Start listens for inbound SRT publishers. The SRT streamid carries the
// stream key (an optional "publish:" prefix is accepted); the same key list
// guards both RTMP and SRT.
func Start(setStreamAsConnected func(*io.PipeReader, string), setBroadcaster func(models.Broadcaster, string), isStreamBusy func(string) bool) {
	_setStreamAsConnected = setStreamAsConnected
	_setBroadcaster = setBroadcaster
	_isStreamBusy = isStreamBusy

	configRepository := configrepository.Get()
	if !configRepository.GetSRTServerEnabled() {
		log.Traceln("SRT ingest is disabled.")
		return
	}
	port := configRepository.GetSRTServerPort()

	config := gosrt.DefaultConfig()
	ln, err := gosrt.Listen("srt", fmt.Sprintf(":%d", port), config)
	if err != nil {
		log.Errorln("unable to start SRT ingest listener:", err)
		return
	}
	log.Tracef("SRT server is listening for incoming streams on udp port: %d", port)

	for {
		conn, mode, err := ln.Accept(acceptConnection)
		if err != nil {
			log.Debugln("SRT accept error:", err)
			time.Sleep(time.Second)
			continue
		}
		if mode != gosrt.PUBLISH || conn == nil {
			continue
		}
		go handlePublisher(conn)
	}
}

// acceptConnection validates the stream key carried in the SRT streamid.
func acceptConnection(req gosrt.ConnRequest) gosrt.ConnType {
	key := streamKeyFromStreamID(req.StreamId())
	if key == "" {
		log.Errorln("SRT connection with unknown or missing stream key rejected from", req.RemoteAddr().String())
		return gosrt.REJECT
	}
	if _isStreamBusy(key) {
		log.Errorln("stream already running; rejecting incoming SRT stream from", req.RemoteAddr().String())
		return gosrt.REJECT
	}
	return gosrt.PUBLISH
}

// streamKeyFromStreamID returns the matched stream key, or "" if invalid.
func streamKeyFromStreamID(streamID string) string {
	streamID = strings.TrimPrefix(streamID, "publish:")
	streamID = strings.TrimPrefix(streamID, "publish/")
	configRepository := configrepository.Get()
	for _, key := range configRepository.GetStreamKeys() {
		if key.Key != nil && *key.Key != "" && *key.Key == streamID {
			return *key.Key
		}
	}
	return ""
}

func handlePublisher(conn gosrt.Conn) {
	key := streamKeyFromStreamID(conn.StreamId())
	if key == "" || _isStreamBusy(key) {
		_ = conn.Close()
		return
	}

	remoteAddr := conn.RemoteAddr().String()
	log.Infoln("Inbound SRT stream connected from", remoteAddr)

	broadcaster := models.Broadcaster{
		RemoteAddr: remoteAddr,
		Time:       time.Now(),
		StreamDetails: models.InboundStreamDetails{
			Encoder: "SRT",
		},
	}
	_setBroadcaster(broadcaster, key)

	// Sniff the audio codec BEFORE the transcoder spawns: its fMP4 path
	// picks a bitstream filter per audio codec, and picking wrong kills the
	// session at startup. Bounded — whatever arrives inside a second, capped
	// well under the details capture — and a failed sniff just leaves the
	// codec unknown, which the transcoder treats as it always has. The head
	// is not lost: it seeds both the pipe and the details capture below.
	const headMax = 256 << 10
	head := make([]byte, 0, headMax)
	{
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		hb := make([]byte, 1316)
		for len(head) < headMax {
			n, err := conn.Read(hb)
			if n > 0 {
				head = append(head, hb[:n]...)
			}
			if err != nil {
				break
			}
		}
		_ = conn.SetReadDeadline(time.Time{})
	}
	config.SetInboundAudioCodec(probeAudioCodec(head))

	pipeOut, pipeIn := io.Pipe()

	_mu.Lock()
	_activeConn = conn
	_activePipe = pipeIn
	_mu.Unlock()

	_setStreamAsConnected(pipeOut, key)

	// The transcoder consumes the raw container from the pipe; ffmpeg probes
	// it itself, so no demuxing happens here. A copy of the stream does get
	// kept aside for ffprobe runs that fill in the broadcaster details the
	// admin shows — enough bytes for parameter sets, frames and a usable
	// bitrate window at 4K rates, capped by time so a thin stream still
	// qualifies. A fresh capture every 30s keeps the shown numbers tracking
	// a VBR source instead of freezing the connect-time estimate — but only
	// for mpegts, which self-syncs from any byte. A matroska/NUT chunk cut
	// mid-stream has no header to parse from, so those streams keep their
	// connect-time measurement.
	const probeTarget = 4 << 20
	const probeFloor = 128 << 10
	const reprobeAfter = 30 * time.Second
	probeBuf := make([]byte, 0, probeTarget)
	captureStart := time.Now()
	var resumeAt time.Time // zero while a capture is filling
	var reprobe atomic.Bool
	reprobe.Store(true)

	if len(head) > 0 {
		if _, werr := pipeIn.Write(head); werr != nil {
			log.Infoln("Inbound SRT stream disconnected.")
			teardown(conn, pipeIn)
			return
		}
		probeBuf = append(probeBuf, head...)
	}

	buffer := make([]byte, 1316) // 7 mpegts packets, the SRT payload convention
	for {
		n, err := conn.Read(buffer)
		if n > 0 {
			if _, werr := pipeIn.Write(buffer[:n]); werr != nil {
				break
			}
			if resumeAt.IsZero() {
				if room := probeTarget - len(probeBuf); room > 0 {
					probeBuf = append(probeBuf, buffer[:min(n, room)]...)
				}
				if len(probeBuf) >= probeTarget ||
					(len(probeBuf) >= probeFloor && time.Since(captureStart) > 3*time.Second) {
					prefix := probeBuf
					probeBuf = make([]byte, 0, probeTarget)
					resumeAt = time.Now().Add(reprobeAfter)
					go func() {
						if details, container := probeInboundStream(prefix); container != "" {
							b := broadcaster
							b.StreamDetails = details
							_setBroadcaster(b, key)
							if container != "mpegts" {
								reprobe.Store(false)
							}
						}
					}()
				}
			} else if reprobe.Load() && time.Now().After(resumeAt) {
				resumeAt = time.Time{}
				captureStart = time.Now()
			}
		}
		if err != nil {
			break
		}
	}

	log.Infoln("Inbound SRT stream disconnected.")
	teardown(conn, pipeIn)
}

func teardown(conn gosrt.Conn, pipe *io.PipeWriter) {
	_mu.Lock()
	defer _mu.Unlock()
	_ = conn.Close()
	_ = pipe.Close()
	if _activeConn == conn {
		_activeConn = nil
		_activePipe = nil
	}
	// The sniffed codec belongs to the session that just ended; a following
	// RTMP broadcast must not inherit it.
	config.SetInboundAudioCodec("")
}

// Disconnect will force disconnect the current inbound SRT connection.
func Disconnect() {
	_mu.Lock()
	conn, pipe := _activeConn, _activePipe
	_activeConn = nil
	_activePipe = nil
	_mu.Unlock()
	if conn == nil {
		return
	}
	log.Traceln("Inbound SRT stream disconnect requested.")
	_ = conn.Close()
	if pipe != nil {
		_ = pipe.Close()
	}
}
