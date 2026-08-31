// Package srt implements the SRT/mpegts ingest — the preferred inbound
// path (docs/design.md §2): mpegts carries AV1 and HEVC natively, so the
// Enhanced-RTMP tagging problem never exists here.
package srt

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	gosrt "github.com/datarhei/gosrt"
	log "github.com/sirupsen/logrus"

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
			Encoder: "SRT/mpegts",
		},
	}
	_setBroadcaster(broadcaster, key)

	pipeOut, pipeIn := io.Pipe()

	_mu.Lock()
	_activeConn = conn
	_activePipe = pipeIn
	_mu.Unlock()

	_setStreamAsConnected(pipeOut, key)

	// The transcoder consumes raw mpegts from the pipe; ffmpeg probes the
	// container itself, so no demuxing happens here. A copy of the opening
	// prefix does get kept aside for one ffprobe run that fills in the
	// broadcaster details the admin shows — enough bytes for parameter sets
	// and a few frames, capped by time so a thin stream still gets probed.
	const probeTarget = 2 << 20
	const probeFloor = 128 << 10
	probeBuf := make([]byte, 0, probeTarget)
	probeStart := time.Now()
	probed := false

	buffer := make([]byte, 1316) // 7 mpegts packets, the SRT payload convention
	for {
		n, err := conn.Read(buffer)
		if n > 0 {
			if _, werr := pipeIn.Write(buffer[:n]); werr != nil {
				break
			}
			if !probed {
				if room := probeTarget - len(probeBuf); room > 0 {
					probeBuf = append(probeBuf, buffer[:min(n, room)]...)
				}
				if len(probeBuf) >= probeTarget ||
					(len(probeBuf) >= probeFloor && time.Since(probeStart) > 3*time.Second) {
					probed = true
					prefix := probeBuf
					probeBuf = nil
					go func() {
						if details, ok := probeInboundStream(prefix); ok {
							broadcaster.StreamDetails = details
							_setBroadcaster(broadcaster, key)
						}
					}()
				}
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
