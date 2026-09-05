package core

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"streamingestarr/config"
	"streamingestarr/core/relay"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/persistence/configrepository"
)

// The relay, room side: a feed per broadcast that the transcoder writes
// the transport stream to, the RTSP publisher on top of it, and the
// counts of who is pulling — HTTP transport-stream readers, RTSP
// sessions, and HLS players that fetched a playlist lately.

// relayHLSPlayerTTL is how long an HLS puller counts after its last
// playlist request.
const relayHLSPlayerTTL = 20 * time.Second

// Relay encodings, for the status line.
const (
	RelayEncodingPassthrough = "passthrough"
	RelayEncodingTranscode   = "transcode"
	RelayEncodingForeign     = "passthrough-foreign" // not H.264, and the fallback is off
	RelayEncodingNone        = ""
)

type roomRelay struct {
	mu        sync.Mutex
	feed      *relay.Feed
	encoding  string
	tsPlayers atomic.Int32
	hls       map[string]time.Time
}

// relayRTSP is the one RTSP server for every room; nil when its port
// could not be bound.
var relayRTSP *relay.RTSPServer

// startRelayRTSP binds the RTSP outlet.
func startRelayRTSP() {
	port := configrepository.Get().GetRelayRTSPPort()
	s, err := relay.StartRTSP(port, resolveRelayPath)
	if err != nil {
		log.Errorf("Relay RTSP could not listen on tcp port %d: %v — rtspt:// links will not work until it can", port, err)
		return
	}
	relayRTSP = s
	log.Infof("Relay RTSP is listening on tcp port %d", port)
}

// RelayRTSPListening says whether the RTSP outlet bound its port.
func RelayRTSPListening() bool {
	return relayRTSP != nil
}

// resolveRelayPath maps an RTSP path /<room>/<token> to a room that
// relays over RTSP with that token; "" otherwise.
func resolveRelayPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 2 {
		return ""
	}
	ch := channelrepository.GetChannel(parts[0])
	if ch == nil || !RelayTokenMatches(ch, parts[1]) || !relayHas(ch, models.RelayProtocolRTSP) {
		return ""
	}
	return ch.ID
}

// RelayTokenMatches compares a link's token with the room's, in constant time.
func RelayTokenMatches(ch *models.Channel, token string) bool {
	return ch != nil && ch.Relays() && ch.RelayToken != "" &&
		subtle.ConstantTimeCompare([]byte(token), []byte(ch.RelayToken)) == 1
}

func relayHas(ch *models.Channel, protocol string) bool {
	for _, p := range ch.EffectiveRelayProtocols() {
		if p == protocol {
			return true
		}
	}
	return false
}

// relayVideoMode decides what the relay output does with the video:
// copy when the source is H.264, a re-encode when it is not and the
// room relays and the fallback is on, copy regardless otherwise.
func (c *ChannelRuntime) relayVideoMode() (mode, encoding string) {
	codec := config.GetInboundVideoCodec(c.ID)
	if codec == "" || codec == "h264" {
		return "copy", RelayEncodingPassthrough
	}
	ch := channelrepository.GetChannel(c.ID)
	if ch != nil && ch.Relays() && configrepository.Get().GetRelayTranscodeFallback() {
		return "h264", RelayEncodingTranscode
	}
	// AV1 does not ride MPEG-TS: a copy would make ffmpeg refuse the whole
	// command and end the broadcast. No relay output for this one.
	if codec == "av1" {
		return "", RelayEncodingNone
	}
	return "copy", RelayEncodingForeign
}

// relayBegin opens the feed for a broadcast and returns the ffmpeg
// output URL plus the video mode; "" when the feed could not open.
func (c *ChannelRuntime) relayBegin() (url, video string) {
	feed, err := relay.Listen(c.ID)
	if err != nil {
		log.Errorf("relay for room %s: cannot open the feed: %v", c.ID, err)
		return "", ""
	}
	video, encoding := c.relayVideoMode()
	if video == "" {
		log.Warnf("relay for room %s: the source is %s, which MPEG-TS cannot carry, and the transcode fallback is off — no relay for this broadcast", c.ID, config.GetInboundVideoCodec(c.ID))
		feed.Stop()
		return "", ""
	}
	c.relay.mu.Lock()
	if c.relay.feed != nil {
		c.relay.feed.Stop()
	}
	c.relay.feed = feed
	c.relay.encoding = encoding
	c.relay.mu.Unlock()
	switch encoding {
	case RelayEncodingPassthrough:
		log.Infof("relay for room %s: passthrough (%s) — the links carry the source as it is", c.ID, firstNonEmpty(config.GetInboundVideoCodec(c.ID), "h264"))
	case RelayEncodingTranscode:
		log.Infof("relay for room %s: re-encoding %s to H.264 for the relay links", c.ID, config.GetInboundVideoCodec(c.ID))
	case RelayEncodingForeign:
		log.Warnf("relay for room %s: the source is %s and the transcode fallback is off — PC players that only take H.264 will not play the relay", c.ID, config.GetInboundVideoCodec(c.ID))
	}
	if relayRTSP != nil {
		relayRTSP.Publish(c.ID, feed.Fan)
	}
	return feed.URL(), video
}

// relayEnd closes the feed: the broadcast ended.
func (c *ChannelRuntime) relayEnd() {
	c.relay.mu.Lock()
	feed := c.relay.feed
	c.relay.feed = nil
	c.relay.encoding = RelayEncodingNone
	c.relay.mu.Unlock()
	if relayRTSP != nil {
		relayRTSP.Unpublish(c.ID)
	}
	if feed != nil {
		feed.Stop()
	}
}

// RelayEncoding says what the running broadcast's relay does with the
// video: passthrough, transcode, passthrough-foreign, or "" offline.
func (c *ChannelRuntime) RelayEncoding() string {
	c.relay.mu.Lock()
	defer c.relay.mu.Unlock()
	return c.relay.encoding
}

// RelayPlayerCount is how many relay players pull this room right now.
func (c *ChannelRuntime) RelayPlayerCount() int {
	if c == nil {
		return 0
	}
	n := int(c.relay.tsPlayers.Load())
	if relayRTSP != nil {
		n += relayRTSP.Players(c.ID)
	}
	c.relay.mu.Lock()
	now := time.Now()
	for k, t := range c.relay.hls {
		if now.Sub(t) > relayHLSPlayerTTL {
			delete(c.relay.hls, k)
		}
	}
	n += len(c.relay.hls)
	c.relay.mu.Unlock()
	return n
}

// DropRelayPlayers ends every relay player's connection — the token rotated.
func (c *ChannelRuntime) DropRelayPlayers() {
	if c == nil {
		return
	}
	c.relay.mu.Lock()
	feed := c.relay.feed
	c.relay.hls = nil
	c.relay.mu.Unlock()
	if feed != nil {
		feed.Fan.DropAll()
	}
	if relayRTSP != nil {
		relayRTSP.DropPlayers(c.ID)
	}
}

// MarkRelayHLSPlayer notes an HLS playlist fetch from one player.
func (c *ChannelRuntime) MarkRelayHLSPlayer(key string) {
	c.relay.mu.Lock()
	if c.relay.hls == nil {
		c.relay.hls = map[string]time.Time{}
	}
	c.relay.hls[key] = time.Now()
	c.relay.mu.Unlock()
}

// ServeRelayTS streams the room's transport stream to one HTTP player:
// GET /relay/<room>/<token>.ts. The response never ends while the
// broadcast runs; a player that falls behind is dropped.
func (c *ChannelRuntime) ServeRelayTS(w http.ResponseWriter, r *http.Request) {
	c.relay.mu.Lock()
	feed := c.relay.feed
	c.relay.mu.Unlock()
	if feed == nil {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "the stream is not live", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported here", http.StatusInternalServerError)
		return
	}
	client := feed.Fan.Subscribe(r.RemoteAddr)
	defer client.Close()
	c.relay.tsPlayers.Add(1)
	defer c.relay.tsPlayers.Add(-1)

	h := w.Header()
	h.Set("Content-Type", "video/mp2t")
	h.Set("Cache-Control", "no-store")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-client.Done():
			return
		case chunk := <-client.Packets():
			if _, err := w.Write(chunk); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
