package relay

import (
	"io"
	"net"
	"sync"

	log "github.com/sirupsen/logrus"
)

// Feed is the transcoder's way into a fan-out: a loopback TCP listener
// ffmpeg writes the room's transport stream to as a second output. One
// per broadcast; the listener is up before ffmpeg is spawned, so the
// output can never fail to connect.
type Feed struct {
	Fan  *Fanout
	ln   net.Listener
	mu   sync.Mutex
	conn net.Conn
	done chan struct{}
}

// Listen opens a feed on a free loopback port.
func Listen(room string) (*Feed, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	f := &Feed{Fan: NewFanout(), ln: ln, done: make(chan struct{})}
	go f.serve(room)
	return f, nil
}

// URL is what ffmpeg is told to write to.
func (f *Feed) URL() string {
	return "tcp://" + f.ln.Addr().String()
}

func (f *Feed) serve(room string) {
	defer close(f.done)
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		if f.conn != nil {
			_ = f.conn.Close()
		}
		f.conn = conn
		f.mu.Unlock()
		// Drain as fast as it comes: the fan-out never blocks, so a slow
		// outlet can never back up into the transcoder.
		buf := make([]byte, 64*1024)
		n, err := io.CopyBuffer(f.Fan, conn, buf)
		log.Debugf("relay feed for room %s ended after %d bytes (%v)", room, n, err)
		_ = conn.Close()
	}
}

// Stop closes the listener and ends the fan-out: the broadcast is over.
func (f *Feed) Stop() {
	_ = f.ln.Close()
	f.mu.Lock()
	if f.conn != nil {
		_ = f.conn.Close()
	}
	f.mu.Unlock()
	f.Fan.End()
}
