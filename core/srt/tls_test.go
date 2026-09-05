package srt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeSelfSigned writes a fresh self-signed ECDSA pair for cn to the two
// paths. Every call makes a different certificate (new key, new serial).
func writeSelfSigned(t *testing.T, certPath, keyPath, cn string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
}

// bump moves the files' mtimes forward so a rewrite is visible even on a
// filesystem with coarse timestamps.
func bump(t *testing.T, offset time.Duration, paths ...string) {
	t.Helper()
	ts := time.Now().Add(offset)
	for _, p := range paths {
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
}

func leafCN(t *testing.T, cert *tls.Certificate) string {
	t.Helper()
	leaf, err := leafOf(cert)
	if err != nil {
		t.Fatal(err)
	}
	return leaf.Subject.CommonName
}

func TestPeekClassifiesTLSClientHello(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	hello := []byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x01}
	go func() { _, _ = client.Write(hello) }()

	pc := newPeekConn(server)
	first, err := pc.peekFirstByte()
	if err != nil {
		t.Fatal(err)
	}
	if !isTLSClientHello(first) {
		t.Fatalf("first byte 0x%02x not classified as TLS", first)
	}
	// The peeked byte is replayed: the whole hello reads back.
	got := make([]byte, len(hello))
	if _, err := io.ReadFull(pc, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(hello) {
		t.Fatalf("replayed bytes differ: %x vs %x", got, hello)
	}
}

func TestPeekReplaysPlaintextPreamble(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go func() { _, _ = client.Write([]byte("SGR-TS/1 k\n")) }()

	pc := newPeekConn(server)
	first, err := pc.peekFirstByte()
	if err != nil {
		t.Fatal(err)
	}
	if isTLSClientHello(first) {
		t.Fatalf("plaintext preamble classified as TLS")
	}
	// The byte-by-byte preamble reader must see the line intact, first
	// byte included.
	var line []byte
	b := make([]byte, 1)
	for {
		if _, err := pc.Read(b); err != nil {
			t.Fatal(err)
		}
		if b[0] == '\n' {
			break
		}
		line = append(line, b[0])
	}
	if string(line) != "SGR-TS/1 k" {
		t.Fatalf("preamble read back as %q", line)
	}
}

// expectClosed asserts the peer closed the connection: the next read
// returns EOF (or a closed-pipe error) instead of hanging.
func expectClosed(t *testing.T, c net.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := c.Read(make([]byte, 1))
	if err == nil || (!errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe)) {
		t.Fatalf("expected the server to close the connection, read returned %v", err)
	}
}

func TestRequireModeClosesPlaintext(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	result := make(chan bool, 1)
	go func() {
		_, _, ok := negotiateTransport(server, "test", TLSModeRequire, nil)
		result <- ok
	}()
	if _, err := client.Write([]byte("SGR-TS/1 k\n")); err != nil {
		t.Fatal(err)
	}
	if ok := <-result; ok {
		t.Fatal("plaintext accepted in require mode")
	}
	expectClosed(t, client)
}

func TestOffModeClosesTLSAttempt(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	result := make(chan bool, 1)
	go func() {
		_, _, ok := negotiateTransport(server, "test", TLSModeOff, nil)
		result <- ok
	}()
	if _, err := client.Write([]byte{0x16, 0x03, 0x01}); err != nil {
		t.Fatal(err)
	}
	if ok := <-result; ok {
		t.Fatal("TLS ClientHello accepted with the mode off")
	}
	expectClosed(t, client)
}

func TestAllowModeWithoutCertificateClosesTLSKeepsPlaintext(t *testing.T) {
	// TLS attempt: refused (no certificate — never a plaintext fallback).
	client, server := net.Pipe()
	result := make(chan bool, 1)
	go func() {
		_, _, ok := negotiateTransport(server, "test", TLSModeAllow, nil)
		result <- ok
	}()
	if _, err := client.Write([]byte{0x16, 0x03, 0x01}); err != nil {
		t.Fatal(err)
	}
	if ok := <-result; ok {
		t.Fatal("TLS accepted without a certificate")
	}
	expectClosed(t, client)
	client.Close()

	// A reloader whose files never load counts as "no certificate" too.
	dir := t.TempDir()
	r := newCertReloader(filepath.Join(dir, "missing.crt"), filepath.Join(dir, "missing.key"))
	client, server = net.Pipe()
	defer client.Close()
	go func() {
		_, _, ok := negotiateTransport(server, "test", TLSModeAllow, r)
		result <- ok
	}()
	if _, err := client.Write([]byte{0x16, 0x03, 0x01}); err != nil {
		t.Fatal(err)
	}
	if ok := <-result; ok {
		t.Fatal("TLS accepted with an unloadable certificate")
	}
	expectClosed(t, client)

	// Plaintext still works in allow mode, preamble intact.
	type outcome struct {
		conn      net.Conn
		transport string
		ok        bool
	}
	client2, server2 := net.Pipe()
	defer client2.Close()
	out := make(chan outcome, 1)
	go func() {
		c, tr, ok := negotiateTransport(server2, "test", TLSModeAllow, r)
		out <- outcome{c, tr, ok}
	}()
	go func() { _, _ = client2.Write([]byte("SGR-TS/1 k\n")) }()
	o := <-out
	if !o.ok || o.transport != "TCP" {
		t.Fatalf("plaintext in allow mode: ok=%v transport=%q", o.ok, o.transport)
	}
	line := make([]byte, 11)
	if _, err := io.ReadFull(o.conn, line); err != nil {
		t.Fatal(err)
	}
	if string(line) != "SGR-TS/1 k\n" {
		t.Fatalf("preamble after negotiation: %q", line)
	}
}

func TestAllowModeTLSHandshakeAndPreamble(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "a.crt"), filepath.Join(dir, "a.key")
	writeSelfSigned(t, certPath, keyPath, "ingest.example")
	r := newCertReloader(certPath, keyPath)

	client, server := net.Pipe()
	defer client.Close()
	type outcome struct {
		line      string
		transport string
		ok        bool
	}
	out := make(chan outcome, 1)
	go func() {
		c, tr, ok := negotiateTransport(server, "test", TLSModeAllow, r)
		if !ok {
			out <- outcome{"", tr, false}
			return
		}
		line := make([]byte, 11)
		_, err := io.ReadFull(c, line)
		if err != nil {
			out <- outcome{err.Error(), tr, false}
			return
		}
		out <- outcome{string(line), tr, true}
	}()

	// Wrong SNI on purpose: the ingest serves its one certificate to any
	// name, so the handshake must still complete.
	tc := tls.Client(client, &tls.Config{InsecureSkipVerify: true, ServerName: "somebody-else.example", MinVersion: tls.VersionTLS12}) // #nosec G402 test
	if err := tc.Handshake(); err != nil {
		t.Fatal("client handshake:", err)
	}
	if got := tc.ConnectionState().PeerCertificates[0].Subject.CommonName; got != "ingest.example" {
		t.Fatalf("server presented %q", got)
	}
	if _, err := tc.Write([]byte("SGR-TS/1 k\n")); err != nil {
		t.Fatal(err)
	}
	o := <-out
	if !o.ok || o.transport != "TCP+TLS" || o.line != "SGR-TS/1 k\n" {
		t.Fatalf("TLS negotiation: ok=%v transport=%q line=%q", o.ok, o.transport, o.line)
	}
}

func TestCertReloaderFollowsRenewalAndKeepsLastGood(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "s.crt"), filepath.Join(dir, "s.key")
	writeSelfSigned(t, certPath, keyPath, "first.example")
	r := newCertReloader(certPath, keyPath)

	cert, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cn := leafCN(t, cert); cn != "first.example" {
		t.Fatalf("initial load: %q", cn)
	}

	// Renewal: both files rewritten → the next handshake sees the new one.
	writeSelfSigned(t, certPath, keyPath, "second.example")
	bump(t, 2*time.Second, certPath, keyPath)
	cert, err = r.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cn := leafCN(t, cert); cn != "second.example" {
		t.Fatalf("after renewal: %q", cn)
	}

	// Corrupt certificate file → last good pair is kept, no error.
	if err := os.WriteFile(certPath, []byte("this is not PEM\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bump(t, 4*time.Second, certPath)
	for i := 0; i < 2; i++ { // twice: the second call hits the "same failure" path
		cert, err = r.GetCertificate(nil)
		if err != nil {
			t.Fatal("corrupt file must not surface as an error:", err)
		}
		if cn := leafCN(t, cert); cn != "second.example" {
			t.Fatalf("after corruption (call %d): %q", i, cn)
		}
	}

	// Key that does not match the certificate → still the last good pair.
	writeSelfSigned(t, certPath, filepath.Join(dir, "other.key"), "third.example")
	bump(t, 6*time.Second, certPath)
	cert, err = r.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cn := leafCN(t, cert); cn != "second.example" {
		t.Fatalf("after mismatch: %q", cn)
	}

	// Fixed again → picked up.
	writeSelfSigned(t, certPath, keyPath, "fourth.example")
	bump(t, 8*time.Second, certPath, keyPath)
	cert, err = r.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cn := leafCN(t, cert); cn != "fourth.example" {
		t.Fatalf("after fix: %q", cn)
	}
}

func TestCertReloaderErrorsNameTheFile(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "nope.crt"), filepath.Join(dir, "nope.key")
	r := newCertReloader(certPath, keyPath)
	if _, err := r.GetCertificate(nil); err == nil || !strings.Contains(err.Error(), certPath) {
		t.Fatalf("missing file error must name the path: %v", err)
	}
	if _, err := InspectTLSPair(certPath, keyPath); err == nil || !strings.Contains(err.Error(), certPath) {
		t.Fatalf("InspectTLSPair must name the path: %v", err)
	}

	writeSelfSigned(t, certPath, keyPath, "ok.example")
	info, err := InspectTLSPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Subject != "CN=ok.example" || info.Expired || info.NotAfter.Before(time.Now()) {
		t.Fatalf("unexpected info: %+v", info)
	}

	// Cert/key mismatch on save: exact reason, both paths.
	writeSelfSigned(t, certPath, filepath.Join(dir, "other.key"), "mismatch.example")
	if _, err := InspectTLSPair(certPath, keyPath); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatch must be reported as such: %v", err)
	}

	if os.Geteuid() == 0 {
		t.Log("running as root — the permission-denied wording is not testable here")
		return
	}
	writeSelfSigned(t, certPath, keyPath, "locked.example")
	if err := os.Chmod(keyPath, 0); err != nil {
		t.Fatal(err)
	}
	_, err = InspectTLSPair(certPath, keyPath)
	if err == nil || !strings.Contains(err.Error(), keyPath) || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("unreadable key must name the path and say permission: %v", err)
	}
}

func TestPreambleParsingToleratesLegacyPassphrase(t *testing.T) {
	key, extra, ok := parsePreambleLine("SGR-TS/1 abc123")
	if !ok || key != "abc123" || extra != "" {
		t.Fatalf("plain preamble: ok=%v key=%q extra=%q", ok, key, extra)
	}
	key, extra, ok = parsePreambleLine("SGR-TS/1 abc123 old-passphrase\r")
	if !ok || key != "abc123" || extra != "old-passphrase" {
		t.Fatalf("legacy second token must be tolerated: ok=%v key=%q extra=%q", ok, key, extra)
	}
	if _, _, ok := parsePreambleLine("RTMP abc123"); ok {
		t.Fatal("wrong tag accepted")
	}
	if _, _, ok := parsePreambleLine("SGR-TS/1"); ok {
		t.Fatal("tag without a key accepted")
	}
}

func TestNormalizeTLSMode(t *testing.T) {
	for in, want := range map[string]string{"": "off", "off": "off", "allow": "allow", "require": "require", "bogus": "off"} {
		if got := NormalizeTLSMode(in); got != want {
			t.Fatalf("NormalizeTLSMode(%q) = %q, want %q", in, got, want)
		}
	}
}
