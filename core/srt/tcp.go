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
// (`SGR-TS/1 <streamkey>\n`) before the stream bytes. In plaintext the key
// and the media travel in the clear, so either keep the listener
// tailnet-bound or turn on TLS (tcp/tls/mode) — same port, the first byte
// tells a ClientHello from a preamble, and with "require" nothing
// unencrypted gets past the first byte. The tunnel's UDP-MTU problem does
// not apply here: TCP clamps its segment size; only UDP fragments. Off by
// default.
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
	log.Infof("TCP ingest (mpegts, SGR-TS/1 preamble) is listening on tcp port %d", port)
	announceTLSState()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Debugln("TCP ingest accept error:", err)
			continue
		}
		go handleTCPPublisher(conn)
	}
}

// announceTLSState logs, at startup, whether TLS is on and whether the
// certificate loads — so a bad path is visible in the log before the
// first sender finds out. Nothing here is cached: every connection
// re-reads the mode and the paths, and the reloader re-checks the files,
// so a certificate that appears or is fixed later just works.
func announceTLSState() {
	cfg := configrepository.Get()
	mode := NormalizeTLSMode(cfg.GetTCPIngestTLSMode())
	if mode == TLSModeOff {
		return
	}
	certs := tlsReloaderFor(cfg.GetTCPIngestTLSCertFile(), cfg.GetTCPIngestTLSKeyFile())
	var err error
	if certs == nil {
		err = fmt.Errorf("no certificate and key configured")
	} else {
		_, err = certs.current()
	}
	switch {
	case err == nil:
		log.Infof("TCP ingest TLS mode is %q", mode)
	case mode == TLSModeRequire:
		log.Errorf("TCP ingest TLS mode is %q but TLS is unavailable — ALL connections will be refused until the certificate loads: %v", mode, err)
	default:
		log.Errorf("TCP ingest TLS mode is %q but TLS is unavailable — TLS attempts will be refused, plaintext still accepted: %v", mode, err)
	}
}

// handleTCPPublisher enforces the wire contract the sender (Jellystreamerr
// 438f942) speaks: an optional TLS handshake first (mode permitting), then
// exactly one preamble line `SGR-TS/1 <streamkey>\n` within 5 seconds and
// 1KB, then raw container bytes (mpegts normally, matroska for AV1 —
// pump's probe autodetects, same as SRT). Anything malformed, unknown, or
// slow is closed with the remote address logged.
//
// The mode and the certificate paths are read here, per connection, so a
// change in the admin applies to the next sender without a restart;
// sessions already running are untouched by it.
func handleTCPPublisher(raw net.Conn) {
	remoteAddr := raw.RemoteAddr().String()

	cfg := configrepository.Get()
	mode := cfg.GetTCPIngestTLSMode()
	var certs *certReloader
	if NormalizeTLSMode(mode) != TLSModeOff {
		certs = tlsReloaderFor(cfg.GetTCPIngestTLSCertFile(), cfg.GetTCPIngestTLSKeyFile())
	}
	conn, transport, ok := negotiateTransport(raw, remoteAddr, mode, certs)
	if !ok {
		return
	}

	key, ok := readPreamble(conn, remoteAddr)
	if !ok {
		_ = conn.Close()
		return
	}
	if _isStreamBusy(key) {
		log.Errorln("stream already running; rejecting incoming", transport, "stream from", remoteAddr)
		_ = conn.Close()
		return
	}
	pump(conn, remoteAddr, transport, key)
}

// readPreamble reads and validates the one preamble line and returns the
// stream key it named. ok=false means the caller closes the connection;
// the reason is already logged.
//
// Older senders (pre-TLS) appended a passphrase as a second token; TLS
// replaced that lock, and a stray second token is ignored rather than
// rejected so the switch-over never strands a sender.
func readPreamble(conn net.Conn, remoteAddr string) (key string, ok bool) {
	_ = conn.SetReadDeadline(time.Now().Add(firstByteDeadline))
	line := make([]byte, 0, 128)
	b := make([]byte, 1)
	for {
		n, err := conn.Read(b)
		if err != nil {
			log.Errorln("TCP ingest connection from", remoteAddr, "closed before a valid preamble:", err)
			return "", false
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
			return "", false
		}
	}
	_ = conn.SetReadDeadline(time.Time{})

	rawKey, extra, wellFormed := parsePreambleLine(string(line))
	if !wellFormed {
		log.Errorln("TCP ingest connection from", remoteAddr, "rejected — malformed preamble")
		return "", false
	}
	if extra != "" {
		log.Debugln("TCP ingest connection from", remoteAddr, "sent a second preamble token (legacy passphrase) — ignored")
	}
	key = streamKeyFromStreamID(rawKey)
	if key == "" {
		log.Errorln("TCP ingest connection with unknown stream key rejected from", remoteAddr)
		return "", false
	}
	return key, true
}

// parsePreambleLine splits one preamble line (without its trailing
// newline) into the raw stream key and whatever followed it. wellFormed
// is false when the line is not `SGR-TS/1 <something>`. A second token
// is returned, not rejected: older senders put a passphrase there.
func parsePreambleLine(line string) (rawKey, extra string, wellFormed bool) {
	tag, rest, found := strings.Cut(strings.TrimRight(line, "\r"), " ")
	if !found || tag != "SGR-TS/1" {
		return "", "", false
	}
	rawKey, extra, _ = strings.Cut(strings.TrimSpace(rest), " ")
	return strings.TrimSpace(rawKey), strings.TrimSpace(extra), true
}
