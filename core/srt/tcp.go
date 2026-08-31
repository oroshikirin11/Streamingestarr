package srt

import (
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"streamingestarr/models"
	"streamingestarr/persistence/configrepository"
)

// StartTCP listens for inbound mpegts-over-TCP publishers.
//
// Why it exists: SRT's bounded recovery window turns uplink loss storms
// into on-screen artifacts, while TCP retransmits forever — loss becomes
// invisible delay, which a deep-buffered theater happily absorbs. RTMP
// already proved that on this very network, but RTMP cannot carry HEVC or
// AV1; a raw mpegts TCP socket carries anything the pipeline eats (the
// sender is Jellystreamerr 438f942: dial, one preamble line, then bytes).
//
// Trust model: the sender authenticates with one preamble line
// (`SGR-TS/1 <streamkey>\n`) before the stream bytes — but key and media
// travel in PLAINTEXT, so keep the listener tailnet-bound (compose
// publishes it on TAILNET_IP) where the tunnel provides the encryption.
// The tunnel's UDP-MTU problem does not apply here: TCP clamps its
// segment size; only UDP fragments. Off by default.
func StartTCP(setStreamAsConnected func(*io.PipeReader, string), setBroadcaster func(models.Broadcaster, string), isStreamBusy func(string) bool) {
	wire(setStreamAsConnected, setBroadcaster, isStreamBusy)

	configRepository := configrepository.Get()
	if !configRepository.GetTCPIngestEnabled() {
		log.Traceln("TCP ingest is disabled.")
		return
	}
	port := configRepository.GetTCPIngestPort()

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Errorln("unable to start TCP ingest listener:", err)
		return
	}
	log.Infof("TCP ingest (mpegts, tailnet-only trust) is listening on tcp port %d", port)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Debugln("TCP ingest accept error:", err)
			continue
		}
		go handleTCPPublisher(conn.(*net.TCPConn))
	}
}

// handleTCPPublisher enforces the wire contract the sender (Jellystreamerr
// 438f942) speaks: exactly one preamble line `SGR-TS/1 <streamkey>\n`
// within 5 seconds and 1KB, then raw container bytes (mpegts normally,
// matroska for AV1 — pump's probe autodetects, same as SRT). Anything
// malformed, unknown, or slow is closed with the remote address logged.
func handleTCPPublisher(conn *net.TCPConn) {
	remoteAddr := conn.RemoteAddr().String()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line := make([]byte, 0, 128)
	b := make([]byte, 1)
	for {
		n, err := conn.Read(b)
		if err != nil {
			log.Errorln("TCP ingest connection from", remoteAddr, "closed before a valid preamble:", err)
			_ = conn.Close()
			return
		}
		if n == 0 {
			continue
		}
		if b[0] == '\n' {
			break
		}
		line = append(line, b[0])
		if len(line) > 1024 {
			log.Errorln("TCP ingest connection from", remoteAddr, "rejected — preamble exceeds 1KB")
			_ = conn.Close()
			return
		}
	}
	_ = conn.SetReadDeadline(time.Time{})

	tag, rest, ok := strings.Cut(strings.TrimRight(string(line), "\r"), " ")
	if !ok || tag != "SGR-TS/1" {
		log.Errorln("TCP ingest connection from", remoteAddr, "rejected — malformed preamble")
		_ = conn.Close()
		return
	}
	// Optional second token: the TCP passphrase. When one is configured,
	// key alone no longer opens the door — the extra lock for a listener
	// that faces the internet, where the streamid... IS the whole lock.
	rawKey, suppliedPass, _ := strings.Cut(strings.TrimSpace(rest), " ")
	if want := configrepository.Get().GetTCPIngestPassphrase(); want != "" && strings.TrimSpace(suppliedPass) != want {
		log.Errorln("TCP ingest connection from", remoteAddr, "rejected — missing or wrong passphrase")
		_ = conn.Close()
		return
	}
	key := streamKeyFromStreamID(strings.TrimSpace(rawKey))
	if key == "" {
		log.Errorln("TCP ingest connection with unknown stream key rejected from", remoteAddr)
		_ = conn.Close()
		return
	}
	if _isStreamBusy(key) {
		log.Errorln("stream already running; rejecting incoming TCP stream from", remoteAddr)
		_ = conn.Close()
		return
	}
	pump(conn, remoteAddr, "TCP", key)
}
