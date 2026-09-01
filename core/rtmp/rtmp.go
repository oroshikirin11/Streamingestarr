package rtmp

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/nareix/joy5/format/flv"
	"github.com/nareix/joy5/format/flv/flvio"
	log "github.com/sirupsen/logrus"

	"github.com/nareix/joy5/format/rtmp"
	"streamingestarr/config"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/persistence/configrepository"
	"streamingestarr/webserver/handlers/generated"
)

// rtmpSession is one channel's live inbound RTMP connection. Keyed by
// channel so rooms can broadcast concurrently over the same port.
type rtmpSession struct {
	channelID string
	pipe      *io.PipeWriter
	conn      net.Conn
	active    bool
}

var (
	_sessionsMu sync.Mutex
	_sessions   = map[string]*rtmpSession{}
)

var (
	_setStreamAsConnected func(*io.PipeReader, string)
	_setBroadcaster       func(models.Broadcaster, string)
	_isStreamBusy         func(string) bool
)

// channelIDForStreamKey resolves which channel a validated stream key
// feeds: a room's own key routes to that room, the global list to the
// default channel.
func channelIDForStreamKey(key string) string {
	if id := channelrepository.GetChannelIDForKey(key); id != "" {
		return id
	}
	return channelrepository.DefaultChannelID
}

// Start starts the rtmp service, listening on specified RTMP port. The
// callbacks receive the matched stream key so the core can resolve which
// channel the inbound stream feeds.
func Start(setStreamAsConnected func(*io.PipeReader, string), setBroadcaster func(models.Broadcaster, string), isStreamBusy func(string) bool) {
	_setStreamAsConnected = setStreamAsConnected
	_setBroadcaster = setBroadcaster
	_isStreamBusy = isStreamBusy

	configRepository := configrepository.Get()

	port := configRepository.GetRTMPPortNumber()
	s := rtmp.NewServer()
	var lis net.Listener
	var err error
	if lis, err = net.Listen("tcp", fmt.Sprintf(":%d", port)); err != nil {
		log.Fatal(err)
	}

	s.LogEvent = func(c *rtmp.Conn, nc net.Conn, e int) {
		es := rtmp.EventString[e]
		log.Traceln("RTMP", nc.LocalAddr(), nc.RemoteAddr(), es)
	}

	s.HandleConn = HandleConn

	if err != nil {
		log.Panicln(err)
	}
	log.Tracef("RTMP server is listening for incoming stream on port: %d", port)

	for {
		nc, err := lis.Accept()
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		go s.HandleNetConn(nc)
	}
}

// HandleConn is fired when an inbound RTMP connection takes place.
func HandleConn(c *rtmp.Conn, nc net.Conn) {
	configRepository := configrepository.Get()

	accessGranted := false
	streamKey := ""
	validStreamingKeys := configRepository.GetStreamKeys()

	// If a stream key override was specified then use that instead.
	if config.TemporaryStreamKey != "" {
		validStreamingKeys = []generated.StreamKey{{Key: &config.TemporaryStreamKey}}
	}

	for _, key := range validStreamingKeys {
		if key.Key != nil && secretMatch(*key.Key, c.URL.Path) {
			accessGranted = true
			streamKey = *key.Key
			break
		}
	}

	// A room's own key opens the door too (unless the override is active).
	if !accessGranted && config.TemporaryStreamKey == "" {
		if candidate := streamKeyFromPath(c.URL.Path); candidate != "" &&
			channelrepository.GetChannelIDForKey(candidate) != "" {
			accessGranted = true
			streamKey = candidate
		}
	}

	if !accessGranted {
		log.Errorln("invalid streaming key; rejecting incoming stream from", nc.RemoteAddr().String())
		_ = nc.Close()
		return
	}

	// The channel may already be fed over another protocol (SRT/TCP).
	if _isStreamBusy != nil && _isStreamBusy(streamKey) {
		log.Errorln("stream already running; rejecting incoming stream from", nc.RemoteAddr().String())
		_ = nc.Close()
		return
	}

	// Claim the room's RTMP slot atomically — the busy check above is
	// advisory when two publishers race it.
	channelID := channelIDForStreamKey(streamKey)
	session := &rtmpSession{channelID: channelID, conn: nc, active: true}
	_sessionsMu.Lock()
	if _, exists := _sessions[channelID]; exists {
		_sessionsMu.Unlock()
		log.Errorln("stream already running; can not overtake an existing stream from", nc.RemoteAddr().String())
		_ = nc.Close()
		return
	}
	_sessions[channelID] = session
	_sessionsMu.Unlock()

	c.LogTagEvent = func(isRead bool, t flvio.Tag) {
		if t.Type == flvio.TAG_AMF0 {
			log.Tracef("%+v\n", t.DebugFields())
			setCurrentBroadcasterInfo(t, nc.RemoteAddr().String(), streamKey)
		}
	}

	rtmpOut, rtmpIn := io.Pipe()
	_sessionsMu.Lock()
	session.pipe = rtmpIn
	_sessionsMu.Unlock()
	log.Infoln("Inbound stream connected from", nc.RemoteAddr().String(), "feeding room", channelID)
	_setStreamAsConnected(rtmpOut, streamKey)

	w := flv.NewMuxer(rtmpIn)

	for session.isActive() {
		// If we don't get a readable packet in 10 seconds give up and disconnect
		if err := nc.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			log.Debugln(err)
		}

		pkt, err := c.ReadPacket()

		// Broadcaster disconnected
		if err == io.EOF {
			session.disconnect()
			return
		}

		// Read timeout.  Disconnect.
		if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
			log.Debugln("Timeout reading the inbound stream from the broadcaster.  Assuming that they disconnected and ending the stream.")
			session.disconnect()
			return
		}

		if err := w.WritePacket(pkt); err != nil {
			log.Errorln("unable to write rtmp packet", err)
			session.disconnect()
			return
		}
	}
}

func (s *rtmpSession) isActive() bool {
	_sessionsMu.Lock()
	defer _sessionsMu.Unlock()
	return s.active
}

// disconnect ends the session and frees the room's RTMP slot. Idempotent —
// the read loop and a forced Disconnect may both arrive here.
func (s *rtmpSession) disconnect() {
	_sessionsMu.Lock()
	if !s.active {
		_sessionsMu.Unlock()
		return
	}
	s.active = false
	if _sessions[s.channelID] == s {
		delete(_sessions, s.channelID)
	}
	_sessionsMu.Unlock()

	log.Infoln("Inbound stream for room", s.channelID, "disconnected.")
	_ = s.conn.Close()
	if s.pipe != nil {
		_ = s.pipe.Close()
	}
}

// Disconnect force-disconnects the channel's inbound RTMP connection, if any.
func Disconnect(channelID string) {
	_sessionsMu.Lock()
	s := _sessions[channelID]
	_sessionsMu.Unlock()
	if s == nil {
		return
	}
	log.Traceln("Inbound stream disconnect requested for room", channelID)
	s.disconnect()
}
