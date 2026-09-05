// Package relay hands a room's broadcast to external players: a
// transport-stream fan-out fed by the transcoder, and the outlets that
// read from it — MPEG-TS over HTTP, HLS by token, and RTSP.
package relay

import (
	"sync"
)

const (
	packetSize = 188
	syncByte   = 0x47
	// maxGOPBytes bounds the join buffer: a 20 Mbit/s stream for ten
	// seconds. A source without keyframes inside that window still joins,
	// just not cleanly.
	maxGOPBytes = 24 << 20
	// clientQueue is how many chunks a slow reader may fall behind before
	// it is dropped — the fan-out never blocks the transcoder.
	clientQueue = 512

	streamTypeH264  = 0x1b
	streamTypeH265  = 0x24
	streamTypeMPEG2 = 0x02
	streamTypeAAC   = 0x0f
	streamTypeLATM  = 0x11
	streamTypeAC3   = 0x81
	streamTypeMP3   = 0x03
	streamTypeMP3b  = 0x04
)

// Fanout takes a live MPEG-TS byte stream and serves it to any number of
// readers, each joining at a keyframe with the tables in front. Pure: no
// network, no clock. Safe for one writer and many subscribers.
type Fanout struct {
	mu       sync.Mutex
	leftover []byte

	pat       []byte
	pmt       []byte
	pmtPID    int
	videoPID  int
	videoType byte
	audioType byte

	// gop holds the packets since the last keyframe, keyframe first: what
	// a newcomer gets so its decoder starts at once.
	gop    []byte
	sawKey bool

	clients map[*Client]struct{}
	ended   bool
	bytesIn uint64
}

// Client is one reader of a Fanout.
type Client struct {
	Name string
	fan  *Fanout
	ch   chan []byte
	done chan struct{}
	once sync.Once
	// waiting: joined before a keyframe was seen; fed from the next one.
	waiting bool
	lagged  bool
	// protected: an internal reader (the RTSP publisher) that DropAll
	// leaves alone — it is not a player.
	protected bool
}

// NewFanout makes an empty fan-out.
func NewFanout() *Fanout {
	return &Fanout{clients: map[*Client]struct{}{}}
}

// Write feeds transport-stream bytes, in any chunking. io.Writer.
func (f *Fanout) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ended {
		return len(p), nil
	}
	f.bytesIn += uint64(len(p))
	buf := p
	if len(f.leftover) > 0 {
		buf = append(f.leftover, p...)
		f.leftover = nil
	}

	// Walk whole packets; anything that is not on a sync byte is skipped
	// up to the next pair of sync bytes. Only validated packets are kept,
	// so a glitch on the wire never reaches a player.
	out := make([]byte, 0, len(buf))
	keyAt := -1 // offset of the first keyframe packet in out
	off := 0
	for off+packetSize <= len(buf) {
		if buf[off] != syncByte {
			r := resync(buf[off:])
			if r < 0 {
				off = len(buf) - packetSize + 1
				if off < 0 {
					off = 0
				}
				break
			}
			off += r
			continue
		}
		pkt := buf[off : off+packetSize]
		off += packetSize
		pid := int(pkt[1]&0x1f)<<8 | int(pkt[2])
		switch {
		case pid == 0:
			f.pat = append(f.pat[:0], pkt...)
			f.pmtPID = parsePAT(pkt)
		case f.pmtPID != 0 && pid == f.pmtPID:
			f.pmt = append(f.pmt[:0], pkt...)
			f.videoPID, f.videoType, f.audioType = parsePMT(pkt)
		case f.videoPID != 0 && pid == f.videoPID:
			if isKeyframe(pkt, f.videoType) {
				f.gop = f.gop[:0]
				f.sawKey = true
				if keyAt < 0 {
					keyAt = len(out)
				}
			}
		}
		if f.sawKey {
			f.gop = append(f.gop, pkt...)
		}
		out = append(out, pkt...)
	}
	if off < len(buf) {
		f.leftover = append([]byte{}, buf[off:]...)
	}
	if len(f.gop) > maxGOPBytes {
		// No keyframe in a long while: keep the tail, joins are rough.
		f.gop = append([]byte{}, f.gop[len(f.gop)-maxGOPBytes/2:]...)
	}
	if len(out) == 0 || len(f.clients) == 0 {
		return len(p), nil
	}

	// Deliver: one buffer shared by every active client.
	var fromKey []byte
	if keyAt >= 0 {
		fromKey = out[keyAt:]
	}
	for c := range f.clients {
		if c.waiting {
			if keyAt < 0 {
				continue
			}
			c.waiting = false
			c.offer(f.tables())
			c.offer(fromKey)
			continue
		}
		c.offer(out)
	}
	return len(p), nil
}

// tables is PAT then PMT, the two packets a decoder needs first.
func (f *Fanout) tables() []byte {
	t := make([]byte, 0, len(f.pat)+len(f.pmt))
	t = append(t, f.pat...)
	t = append(t, f.pmt...)
	return t
}

// Subscribe adds a reader. A stream that already has a keyframe gives
// the newcomer the tables and the current group of pictures at once;
// otherwise it starts at the next keyframe.
func (f *Fanout) Subscribe(name string) *Client {
	c := &Client{Name: name, fan: f, ch: make(chan []byte, clientQueue), done: make(chan struct{})}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ended {
		close(c.done)
		return c
	}
	f.clients[c] = struct{}{}
	if f.sawKey && len(f.pat) > 0 && len(f.pmt) > 0 {
		burst := f.tables()
		burst = append(burst, f.gop...)
		c.offer(burst)
	} else {
		c.waiting = true
	}
	return c
}

// offer hands a chunk to a client without ever blocking; a full queue
// ends the client — it is too slow to follow live.
func (c *Client) offer(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	select {
	case c.ch <- chunk:
	default:
		c.lagged = true
		c.closeLocked()
	}
}

// closeLocked ends a client; the fan-out lock is held by the caller.
func (c *Client) closeLocked() {
	c.once.Do(func() {
		delete(c.fan.clients, c)
		close(c.done)
	})
}

// Close ends a client from its own side.
func (c *Client) Close() {
	c.fan.mu.Lock()
	c.closeLocked()
	c.fan.mu.Unlock()
}

// Packets is the client's chunk stream.
func (c *Client) Packets() <-chan []byte { return c.ch }

// Done closes when the client was ended: by itself, for lagging, or
// because the stream ended.
func (c *Client) Done() <-chan struct{} { return c.done }

// Protect marks the client as internal: DropAll skips it.
func (c *Client) Protect() {
	c.fan.mu.Lock()
	c.protected = true
	c.fan.mu.Unlock()
}

// Lagged says whether the client was dropped for falling behind.
func (c *Client) Lagged() bool {
	c.fan.mu.Lock()
	defer c.fan.mu.Unlock()
	return c.lagged
}

// Count is the number of readers right now.
func (f *Fanout) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.clients)
}

// Ready says whether a newcomer would be served at once.
func (f *Fanout) Ready() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sawKey && len(f.pat) > 0 && len(f.pmt) > 0
}

// Codecs names the video and audio codecs the tables declared.
func (f *Fanout) Codecs() (video, audio string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return codecName(f.videoType), codecName(f.audioType)
}

// BytesIn is the total fed so far.
func (f *Fanout) BytesIn() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bytesIn
}

// DropAll ends every player but keeps the stream going — the token
// rotated, the links died. Protected readers stay.
func (f *Fanout) DropAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for c := range f.clients {
		if !c.protected {
			c.closeLocked()
		}
	}
}

// End marks the stream over: every reader ends, no writes are taken.
func (f *Fanout) End() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended = true
	for c := range f.clients {
		c.closeLocked()
	}
}

// --- transport-stream parsing -------------------------------------------

func resync(b []byte) int {
	for i := 0; i+packetSize < len(b); i++ {
		if b[i] == syncByte && b[i+packetSize] == syncByte {
			return i
		}
	}
	return -1
}

// payloadOf returns a packet's payload after any adaptation field, and
// the adaptation field's flags byte (0 when absent).
func payloadOf(pkt []byte) (payload []byte, afFlags byte) {
	afc := (pkt[3] >> 4) & 3
	off := 4
	if afc&2 != 0 {
		afLen := int(pkt[4])
		if afLen > 0 && 5 < len(pkt) {
			afFlags = pkt[5]
		}
		off = 5 + afLen
	}
	if afc&1 == 0 || off > len(pkt) {
		return nil, afFlags
	}
	return pkt[off:], afFlags
}

// sectionOf returns the PSI section in a packet that starts one.
func sectionOf(pkt []byte) []byte {
	if pkt[1]&0x40 == 0 {
		return nil
	}
	payload, _ := payloadOf(pkt)
	if len(payload) < 1 {
		return nil
	}
	pointer := int(payload[0])
	if 1+pointer >= len(payload) {
		return nil
	}
	return payload[1+pointer:]
}

// parsePAT returns the first program's PMT PID, 0 when none.
func parsePAT(pkt []byte) int {
	s := sectionOf(pkt)
	if len(s) < 12 || s[0] != 0x00 {
		return 0
	}
	sectionLen := int(s[1]&0x0f)<<8 | int(s[2])
	end := 3 + sectionLen - 4 // minus CRC
	if end > len(s) {
		end = len(s)
	}
	for i := 8; i+4 <= end; i += 4 {
		program := int(s[i])<<8 | int(s[i+1])
		pid := int(s[i+2]&0x1f)<<8 | int(s[i+3])
		if program != 0 {
			return pid
		}
	}
	return 0
}

// parsePMT returns the video PID and stream type, and the audio type.
func parsePMT(pkt []byte) (videoPID int, videoType byte, audioType byte) {
	s := sectionOf(pkt)
	if len(s) < 12 || s[0] != 0x02 {
		return 0, 0, 0
	}
	sectionLen := int(s[1]&0x0f)<<8 | int(s[2])
	end := 3 + sectionLen - 4
	if end > len(s) {
		end = len(s)
	}
	infoLen := int(s[10]&0x0f)<<8 | int(s[11])
	for i := 12 + infoLen; i+5 <= end; {
		st := s[i]
		pid := int(s[i+1]&0x1f)<<8 | int(s[i+2])
		esLen := int(s[i+3]&0x0f)<<8 | int(s[i+4])
		switch st {
		case streamTypeH264, streamTypeH265, streamTypeMPEG2:
			if videoPID == 0 {
				videoPID, videoType = pid, st
			}
		case streamTypeAAC, streamTypeLATM, streamTypeAC3, streamTypeMP3, streamTypeMP3b:
			if audioType == 0 {
				audioType = st
			}
		}
		i += 5 + esLen
	}
	return videoPID, videoType, audioType
}

// isKeyframe says whether a video packet starts an access unit a decoder
// can begin at: the random-access flag ffmpeg sets on keyframes, or —
// belt and braces — a parameter set or IDR/IRAP NAL in its first bytes.
func isKeyframe(pkt []byte, videoType byte) bool {
	if pkt[1]&0x40 == 0 {
		return false
	}
	payload, afFlags := payloadOf(pkt)
	if afFlags&0x40 != 0 {
		return true
	}
	// Past the PES header: 6 bytes + 3 (flags, header length) + header.
	if len(payload) < 9 || payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
		return false
	}
	hdr := 9 + int(payload[8])
	if hdr > len(payload) {
		return false
	}
	es := payload[hdr:]
	for i := 0; i+3 < len(es); i++ {
		if es[i] != 0 || es[i+1] != 0 || es[i+2] != 1 {
			continue
		}
		b := es[i+3]
		switch videoType {
		case streamTypeH264:
			switch b & 0x1f {
			case 5, 7:
				return true
			case 1:
				return false
			}
		case streamTypeH265:
			t := (b >> 1) & 0x3f
			if (t >= 16 && t <= 21) || t == 32 || t == 33 {
				return true
			}
			if t <= 9 {
				return false
			}
		case streamTypeMPEG2:
			if b == 0xb3 { // sequence header
				return true
			}
		}
	}
	return false
}

func codecName(st byte) string {
	switch st {
	case streamTypeH264:
		return "h264"
	case streamTypeH265:
		return "hevc"
	case streamTypeMPEG2:
		return "mpeg2video"
	case streamTypeAAC, streamTypeLATM:
		return "aac"
	case streamTypeAC3:
		return "ac3"
	case streamTypeMP3, streamTypeMP3b:
		return "mp3"
	}
	return ""
}
