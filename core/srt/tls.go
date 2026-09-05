// Optional TLS on the TCP ingest. Same port as plaintext: the first byte
// of every connection tells the two apart (a TLS ClientHello always
// starts with record type 0x16; the plaintext preamble starts with 'S').
//
// The certificate is meant to be borrowed from the reverse proxy (Caddy
// renews it under its data dir — docs/deploy-vps.md), so it is re-read
// from disk whenever the files change, at handshake time, with the last
// good pair kept when a reload fails. A certificate problem never turns
// into a plaintext fallback.
package srt

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// The three tcp/tls/mode values. Anything unknown is treated as "off".
const (
	TLSModeOff     = "off"     // plaintext only (the historical behaviour)
	TLSModeAllow   = "allow"   // plaintext and TLS, decided per connection
	TLSModeRequire = "require" // TLS only; plaintext senders are closed
)

// NormalizeTLSMode maps a stored/submitted mode onto the three known
// values, defaulting to off.
func NormalizeTLSMode(mode string) string {
	switch mode {
	case TLSModeAllow, TLSModeRequire:
		return mode
	}
	return TLSModeOff
}

// The two deadlines around a fresh connection: the first byte must show
// up within 5 s (as the plaintext preamble always had to), and a TLS
// handshake must complete within 10 s.
const (
	firstByteDeadline    = 5 * time.Second
	tlsHandshakeDeadline = 10 * time.Second
)

// tlsRecordTypeHandshake is the first byte of every TLS ClientHello.
const tlsRecordTypeHandshake = 0x16

// peekConn is a net.Conn whose reads go through a bufio.Reader so the
// first byte can be looked at without being consumed. Everything except
// Read is the wrapped connection's own, so deadlines and Close behave.
type peekConn struct {
	net.Conn
	r *bufio.Reader
}

func newPeekConn(c net.Conn) *peekConn {
	return &peekConn{Conn: c, r: bufio.NewReaderSize(c, 4096)}
}

// Read serves buffered bytes first (the peeked byte among them), then
// reads the wrapped connection — one underlying read per call, so the
// byte-by-byte preamble reader and the deadlines keep working.
func (p *peekConn) Read(b []byte) (int, error) { return p.r.Read(b) }

// peekFirstByte returns the first byte of the connection without
// consuming it.
func (p *peekConn) peekFirstByte() (byte, error) {
	b, err := p.r.Peek(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

// isTLSClientHello classifies a connection by its first byte.
func isTLSClientHello(first byte) bool { return first == tlsRecordTypeHandshake }

// negotiateTransport turns an accepted socket into the connection the
// preamble reader continues on: the plain socket, or a handshaken TLS
// session on top of it, according to the mode and what the peer sent
// first. ok=false means the connection was closed (and the reason logged)
// and the caller is done. The returned transport name is "TCP" or
// "TCP+TLS" — the label pump stamps on logs, ingest events and the
// admin's Encoder line.
func negotiateTransport(raw net.Conn, remoteAddr, mode string, certs *certReloader) (conn net.Conn, transport string, ok bool) {
	mode = NormalizeTLSMode(mode)
	pc := newPeekConn(raw)

	_ = pc.SetReadDeadline(time.Now().Add(firstByteDeadline))
	first, err := pc.peekFirstByte()
	if err != nil {
		log.Errorln("TCP ingest connection from", remoteAddr, "closed before a valid preamble:", err)
		_ = pc.Close()
		return nil, "", false
	}

	if !isTLSClientHello(first) {
		if mode == TLSModeRequire {
			log.Errorln("TCP ingest connection from", remoteAddr, "rejected — this ingest requires TLS")
			_ = pc.Close()
			return nil, "", false
		}
		_ = pc.SetReadDeadline(time.Time{})
		return pc, "TCP", true
	}

	// A TLS ClientHello.
	if mode == TLSModeOff {
		log.Errorln("TCP ingest connection from", remoteAddr, "rejected — TLS handshake attempted but tcp/tls/mode is off")
		_ = pc.Close()
		return nil, "", false
	}
	if certs == nil {
		log.Errorln("TCP ingest connection from", remoteAddr, "rejected — TLS is unavailable (no certificate configured)")
		_ = pc.Close()
		return nil, "", false
	}
	if _, err := certs.current(); err != nil {
		log.Errorln("TCP ingest connection from", remoteAddr, "rejected — TLS is unavailable (no loadable certificate):", err)
		_ = pc.Close()
		return nil, "", false
	}

	tc := tls.Server(pc, certs.serverConfig())
	_ = tc.SetDeadline(time.Now().Add(tlsHandshakeDeadline))
	if err := tc.Handshake(); err != nil {
		log.Errorln("TCP ingest TLS handshake from", remoteAddr, "failed:", err)
		_ = tc.Close()
		return nil, "", false
	}
	_ = tc.SetDeadline(time.Time{})
	return tc, "TCP+TLS", true
}

// isTCPTransport says whether a transport label is the TCP ingest in
// either of its dresses — where it is about the protocol, "TCP" and
// "TCP+TLS" are the same thing.
func isTCPTransport(transport string) bool {
	return transport == "TCP" || transport == "TCP+TLS"
}

// ---- certificate loading ----------------------------------------------

// fileStamp is what changes when a file is rewritten: mtime and size.
type fileStamp struct {
	mod  time.Time
	size int64
}

func stampOf(path string) (fileStamp, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, err
	}
	return fileStamp{mod: fi.ModTime(), size: fi.Size()}, nil
}

// certReloader hands out the certificate for handshakes and re-reads the
// PEM files whenever they change on disk (a renewal), keeping the last
// good pair when the new files do not load.
type certReloader struct {
	certFile, keyFile string

	mu     sync.Mutex
	cert   *tls.Certificate
	loaded [2]fileStamp // the stamps the loaded pair was read under
	// failed remembers the stamps of the last failed load so a persistent
	// failure is logged (and retried) once per change, not per handshake.
	failed    [2]fileStamp
	failedErr error
}

func newCertReloader(certFile, keyFile string) *certReloader {
	return &certReloader{certFile: certFile, keyFile: keyFile}
}

// serverConfig is the tls.Config for one handshake: TLS 1.2+, certificate
// resolved at handshake time. SNI is ignored — whatever name the client
// asks for, the configured certificate is what it gets.
func (r *certReloader) serverConfig() *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: r.GetCertificate,
	}
}

// GetCertificate is the tls.Config hook. It ignores the ClientHello.
func (r *certReloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return r.current()
}

// current returns the certificate to use right now: the loaded pair if
// the files are unchanged, a freshly loaded one if they were rewritten,
// the previous one if the rewrite does not load (yet). An error means no
// certificate has ever loaded.
func (r *certReloader) current() (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cs, cerr := stampOf(r.certFile)
	ks, kerr := stampOf(r.keyFile)
	stamps := [2]fileStamp{cs, ks}
	if cerr == nil && kerr == nil && r.cert != nil && stamps == r.loaded {
		return r.cert, nil
	}

	// Something is new (or nothing is loaded yet). A failure under the
	// same stamps as the last failure is the same failure: do not retry,
	// do not log again.
	if r.failedErr != nil && stamps == r.failed {
		return r.keepOrFail(r.failedErr)
	}

	var err error
	switch {
	case cerr != nil:
		err = describeFileError("certificate", r.certFile, cerr)
	case kerr != nil:
		err = describeFileError("key", r.keyFile, kerr)
	default:
		var cert tls.Certificate
		cert, err = loadPairWithRetry(r.certFile, r.keyFile)
		if err == nil {
			r.cert = &cert
			r.loaded = stamps
			r.failed = [2]fileStamp{}
			r.failedErr = nil
			if leaf, lerr := leafOf(&cert); lerr == nil {
				log.Infof("TCP ingest TLS: loaded certificate for %q, valid until %s", leaf.Subject.String(), leaf.NotAfter.Format(time.RFC3339))
				if time.Now().After(leaf.NotAfter) {
					log.Warnln("TCP ingest TLS: the certificate has EXPIRED — handshakes still happen, but senders that verify will refuse it")
				}
			}
			return r.cert, nil
		}
	}

	r.failed = stamps
	r.failedErr = err
	if r.cert != nil {
		log.Errorln("TCP ingest TLS: certificate reload failed, keeping the previous certificate:", err)
	} else {
		log.Errorln("TCP ingest TLS: no certificate loaded:", err)
	}
	return r.keepOrFail(err)
}

func (r *certReloader) keepOrFail(err error) (*tls.Certificate, error) {
	if r.cert != nil {
		return r.cert, nil
	}
	return nil, err
}

// loadPairWithRetry loads the pair, and on failure tries once more after
// 250 ms: a renewal writes two files one after the other, and a handshake
// that lands between the two sees a mismatched pair for a moment.
func loadPairWithRetry(certFile, keyFile string) (tls.Certificate, error) {
	cert, err := loadPair(certFile, keyFile)
	if err == nil {
		return cert, nil
	}
	time.Sleep(250 * time.Millisecond)
	return loadPair(certFile, keyFile)
}

// loadPair is tls.LoadX509KeyPair with errors that name the file and the
// reason — the admin page shows them verbatim.
func loadPair(certFile, keyFile string) (tls.Certificate, error) {
	if err := checkReadable(certFile); err != nil {
		return tls.Certificate{}, describeFileError("certificate", certFile, err)
	}
	if err := checkReadable(keyFile); err != nil {
		return tls.Certificate{}, describeFileError("key", keyFile, err)
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("certificate %s with key %s: %w", certFile, keyFile, err)
	}
	if _, err := leafOf(&cert); err != nil {
		return tls.Certificate{}, fmt.Errorf("certificate %s: %w", certFile, err)
	}
	return cert, nil
}

func checkReadable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return f.Close()
}

// describeFileError turns an os error into the sentence the admin sees.
// A permission error names the uid the container runs as, because that
// is nearly always the fix.
func describeFileError(what, path string, err error) error {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("cannot read %s file %s: permission denied (the container runs as uid 101 — grant it read access, see docs/deploy-vps.md)", what, path)
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s file %s does not exist (paths are as seen from inside the container)", what, path)
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("cannot read %s file %s: %v", what, path, pe.Err)
	}
	return fmt.Errorf("cannot read %s file %s: %v", what, path, err)
}

func leafOf(cert *tls.Certificate) (*x509.Certificate, error) {
	if cert.Leaf != nil {
		return cert.Leaf, nil
	}
	if len(cert.Certificate) == 0 {
		return nil, errors.New("no certificate in PEM")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	cert.Leaf = leaf
	return leaf, nil
}

// TLSCertInfo is what the admin learns about the configured certificate:
// never key material.
type TLSCertInfo struct {
	Subject  string
	NotAfter time.Time
	Expired  bool
}

// InspectTLSPair loads the pair once, from disk, and describes the leaf.
// The admin handler validates a save with it and the config response
// reports status through it.
func InspectTLSPair(certFile, keyFile string) (TLSCertInfo, error) {
	if certFile == "" || keyFile == "" {
		return TLSCertInfo{}, errors.New("both a certificate and a key file are required")
	}
	cert, err := loadPair(certFile, keyFile)
	if err != nil {
		return TLSCertInfo{}, err
	}
	leaf, err := leafOf(&cert)
	if err != nil {
		return TLSCertInfo{}, err
	}
	return TLSCertInfo{
		Subject:  leaf.Subject.String(),
		NotAfter: leaf.NotAfter,
		Expired:  time.Now().After(leaf.NotAfter),
	}, nil
}

// One reloader per configured pair of paths, shared by every connection
// so the "keep the last good pair" memory survives across handshakes.
// The paths are read per connection; a change makes a fresh reloader.
var (
	_tlsMu       sync.Mutex
	_tlsReloader *certReloader
)

func tlsReloaderFor(certFile, keyFile string) *certReloader {
	if certFile == "" || keyFile == "" {
		return nil
	}
	_tlsMu.Lock()
	defer _tlsMu.Unlock()
	if _tlsReloader == nil || _tlsReloader.certFile != certFile || _tlsReloader.keyFile != keyFile {
		_tlsReloader = newCertReloader(certFile, keyFile)
	}
	return _tlsReloader
}
