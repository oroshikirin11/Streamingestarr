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
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/persistence/configrepository"
)

var (
	_setStreamAsConnected func(*io.PipeReader, string)
	_setBroadcaster       func(models.Broadcaster, string)
	_isStreamBusy         func(string) bool
	_wireOnce             sync.Once
)

// wire installs the core callbacks exactly once — SRT and TCP listeners
// both call it, whichever starts first wins with identical values.
func wire(setStreamAsConnected func(*io.PipeReader, string), setBroadcaster func(models.Broadcaster, string), isStreamBusy func(string) bool) {
	_wireOnce.Do(func() {
		_setStreamAsConnected = setStreamAsConnected
		_setBroadcaster = setBroadcaster
		_isStreamBusy = isStreamBusy
	})
}

// Start listens for inbound SRT publishers. The SRT streamid carries the
// stream key (an optional "publish:" prefix is accepted); the same key list
// guards both RTMP and SRT.
func Start(setStreamAsConnected func(*io.PipeReader, string), setBroadcaster func(models.Broadcaster, string), isStreamBusy func(string) bool) {
	wire(setStreamAsConnected, setBroadcaster, isStreamBusy)

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

// acceptConnection validates the stream key carried in the SRT streamid
// and, when a passphrase is configured, demands encryption with it. The
// passphrase is OPTIONAL: empty keeps today's behavior (unencrypted
// callers welcome, the streamid is the lock). It is read per connection,
// so a change applies to the next handshake without a restart.
func acceptConnection(req gosrt.ConnRequest) gosrt.ConnType {
	// The streamid names the room before any passphrase is judged, so a
	// room's own passphrase can stand in for the global one.
	key := streamKeyFromStreamID(req.StreamId())
	if key == "" {
		log.Errorln("SRT connection with unknown or missing stream key rejected from", req.RemoteAddr().String())
		return gosrt.REJECT
	}
	if passphrase := passphraseForKey(key, configrepository.Get().GetSRTPassphrase()); passphrase != "" {
		if !req.IsEncrypted() {
			log.Errorln("unencrypted SRT connection rejected from", req.RemoteAddr().String(), "— this ingest requires a passphrase")
			return gosrt.REJECT
		}
		if err := req.SetPassphrase(passphrase); err != nil {
			log.Errorln("SRT connection with wrong passphrase rejected from", req.RemoteAddr().String())
			return gosrt.REJECT
		}
	} else if req.IsEncrypted() {
		// The caller encrypts but no passphrase is configured here — the
		// stream would arrive as undecryptable noise, so refuse honestly.
		log.Errorln("encrypted SRT connection rejected from", req.RemoteAddr().String(), "— no passphrase is configured on this ingest")
		return gosrt.REJECT
	}
	if _isStreamBusy(key) {
		log.Errorln("stream already running; rejecting incoming SRT stream from", req.RemoteAddr().String())
		return gosrt.REJECT
	}
	return gosrt.PUBLISH
}

// streamKeyFromStreamID returns the matched stream key, or "" if invalid.
// Both key stores open the door: the global list (feeding the default
// channel) and each room's own key.
func streamKeyFromStreamID(streamID string) string {
	streamID = strings.TrimPrefix(streamID, "publish:")
	streamID = strings.TrimPrefix(streamID, "publish/")
	configRepository := configrepository.Get()
	for _, key := range configRepository.GetStreamKeys() {
		if key.Key != nil && *key.Key != "" && *key.Key == streamID {
			return *key.Key
		}
	}
	if streamID != "" && channelrepository.GetChannelIDForKey(streamID) != "" {
		return streamID
	}
	return ""
}

func handlePublisher(conn gosrt.Conn) {
	key := streamKeyFromStreamID(conn.StreamId())
	if key == "" || _isStreamBusy(key) {
		_ = conn.Close()
		return
	}
	pump(conn, conn.RemoteAddr().String(), "SRT", key)
}
