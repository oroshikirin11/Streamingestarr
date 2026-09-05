package relay

import (
	"bytes"
	"testing"
)

// tsPacket builds one 188-byte packet.
func tsPacket(pid int, pusi bool, rai bool, payload []byte) []byte {
	p := make([]byte, packetSize)
	p[0] = syncByte
	p[1] = byte(pid >> 8 & 0x1f)
	if pusi {
		p[1] |= 0x40
	}
	p[2] = byte(pid)
	off := 4
	if rai {
		p[3] = 0x30 // adaptation + payload
		p[4] = 1
		p[5] = 0x40
		off = 6
	} else {
		p[3] = 0x10
	}
	copy(p[off:], payload)
	for i := off + len(payload); i < packetSize; i++ {
		p[i] = 0xff
	}
	return p
}

func patPacket(pmtPID int) []byte {
	// pointer, table_id, section_length(13), tsid(2), flags, sec, last, program(2), pid(2), crc(4)
	s := []byte{0x00, 0x00, 0xb0, 0x0d, 0x00, 0x01, 0xc1, 0x00, 0x00, 0x00, 0x01, byte(0xe0 | pmtPID>>8), byte(pmtPID), 0, 0, 0, 0}
	return tsPacket(0, true, false, s)
}

func pmtPacket(videoPID int, videoType byte, audioPID int) []byte {
	streams := []byte{videoType, byte(0xe0 | videoPID>>8), byte(videoPID), 0xf0, 0x00,
		streamTypeAAC, byte(0xe0 | audioPID>>8), byte(audioPID), 0xf0, 0x00}
	sectionLen := 9 + len(streams) + 4
	s := []byte{0x00, 0x02, byte(0xb0 | sectionLen>>8), byte(sectionLen), 0x00, 0x01, 0xc1, 0x00, 0x00, 0xe1, 0x00, 0xf0, 0x00}
	s = append(s, streams...)
	s = append(s, 0, 0, 0, 0)
	return tsPacket(0x100, true, false, s)
}

func TestPATAndPMTParse(t *testing.T) {
	f := NewFanout()
	_, _ = f.Write(patPacket(0x100))
	_, _ = f.Write(pmtPacket(0x101, streamTypeH264, 0x102))
	if f.pmtPID != 0x100 || f.videoPID != 0x101 || f.videoType != streamTypeH264 || f.audioType != streamTypeAAC {
		t.Fatalf("tables misread: pmt=%#x video=%#x type=%#x audio=%#x", f.pmtPID, f.videoPID, f.videoType, f.audioType)
	}
	if v, a := f.Codecs(); v != "h264" || a != "aac" {
		t.Fatalf("codec names %q %q", v, a)
	}
}

func TestJoinAtKeyframeWithTablesInFront(t *testing.T) {
	f := NewFanout()
	_, _ = f.Write(patPacket(0x100))
	_, _ = f.Write(pmtPacket(0x101, streamTypeH264, 0x102))
	_, _ = f.Write(tsPacket(0x101, true, false, []byte{0, 0, 1, 0xe0, 0, 0, 0x80, 0x00, 0x00, 0, 0, 1, 0x41})) // a P frame before any keyframe
	if f.Ready() {
		t.Fatal("ready before a keyframe")
	}
	late := f.Subscribe("late")
	if !late.waiting {
		t.Fatal("a client before the first keyframe must wait")
	}
	key := tsPacket(0x101, true, true, []byte{0, 0, 1, 0xe0, 0, 0, 0x80, 0x00, 0x00, 0, 0, 1, 0x65})
	next := tsPacket(0x101, false, false, []byte{1, 2, 3})
	_, _ = f.Write(append(append([]byte{}, key...), next...))
	if !f.Ready() {
		t.Fatal("not ready after a keyframe")
	}
	got := <-late.Packets()
	if !bytes.Equal(got, f.tables()) {
		t.Fatal("the waiting client must get PAT and PMT first")
	}
	got = <-late.Packets()
	if !bytes.Equal(got[:packetSize], key) {
		t.Fatal("then the stream from the keyframe")
	}
	// A newcomer now gets tables + the group of pictures so far.
	fresh := f.Subscribe("fresh")
	burst := <-fresh.Packets()
	want := append(f.tables(), key...)
	want = append(want, next...)
	if !bytes.Equal(burst, want) {
		t.Fatalf("burst mismatch: %d bytes, want %d", len(burst), len(want))
	}
	if f.Count() != 2 {
		t.Fatalf("count %d", f.Count())
	}
}

func TestKeyframeDetectionWithoutRAI(t *testing.T) {
	pes := func(nal byte) []byte { return []byte{0, 0, 1, 0xe0, 0, 0, 0x80, 0x00, 0x00, 0, 0, 1, nal} }
	if !isKeyframe(tsPacket(0x101, true, false, pes(0x65)), streamTypeH264) {
		t.Fatal("IDR not seen")
	}
	if !isKeyframe(tsPacket(0x101, true, false, pes(0x67)), streamTypeH264) {
		t.Fatal("SPS not seen as a keyframe start")
	}
	if isKeyframe(tsPacket(0x101, true, false, pes(0x41)), streamTypeH264) {
		t.Fatal("a P slice is not a keyframe")
	}
	if !isKeyframe(tsPacket(0x101, true, false, pes(0x40)), streamTypeH265) { // type 32 VPS
		t.Fatal("HEVC VPS not seen")
	}
	if !isKeyframe(tsPacket(0x101, true, false, pes(0x26)), streamTypeH265) { // type 19 IDR_W_RADL
		t.Fatal("HEVC IDR not seen")
	}
	if isKeyframe(tsPacket(0x101, false, false, pes(0x65)), streamTypeH264) {
		t.Fatal("a continuation packet never starts a frame")
	}
}

func TestSlowClientIsDroppedAndOthersLive(t *testing.T) {
	f := NewFanout()
	_, _ = f.Write(patPacket(0x100))
	_, _ = f.Write(pmtPacket(0x101, streamTypeH264, 0x102))
	key := tsPacket(0x101, true, true, []byte{0, 0, 1, 0xe0, 0, 0, 0x80, 0, 0, 0, 0, 1, 0x65})
	_, _ = f.Write(key)
	slow := f.Subscribe("slow")
	quick := f.Subscribe("quick")
	for i := 0; i < clientQueue+5; i++ {
		_, _ = f.Write(tsPacket(0x101, false, false, []byte{byte(i)}))
		for len(quick.Packets()) > 0 {
			<-quick.Packets()
		}
	}
	select {
	case <-slow.Done():
	default:
		t.Fatal("the slow client should have been dropped")
	}
	if !slow.Lagged() {
		t.Fatal("dropped for lag must say so")
	}
	if f.Count() != 1 {
		t.Fatalf("count %d, want the quick client only", f.Count())
	}
	f.End()
	select {
	case <-quick.Done():
	default:
		t.Fatal("End must close every client")
	}
	if c := f.Subscribe("after"); c.Done() == nil {
		t.Fatal("no done channel")
	} else {
		select {
		case <-c.Done():
		default:
			t.Fatal("a subscription after End must be closed at once")
		}
	}
}

func TestPartialWritesAndResync(t *testing.T) {
	f := NewFanout()
	pkt := patPacket(0x100)
	_, _ = f.Write(pkt[:100])
	if f.pmtPID != 0 {
		t.Fatal("half a packet must not parse")
	}
	_, _ = f.Write(pkt[100:])
	if f.pmtPID != 0x100 {
		t.Fatal("the joined packet must parse")
	}
	// Garbage in front of a packet pair is skipped.
	g := NewFanout()
	junk := append([]byte{1, 2, 3}, patPacket(0x100)...)
	junk = append(junk, pmtPacket(0x201, streamTypeH265, 0x202)...)
	_, _ = g.Write(junk)
	if g.pmtPID != 0x100 || g.videoType != streamTypeH265 {
		t.Fatalf("resync failed: pmt=%#x type=%#x", g.pmtPID, g.videoType)
	}
}

func TestDropAllKeepsProtectedReaders(t *testing.T) {
	f := NewFanout()
	_, _ = f.Write(patPacket(0x100))
	_, _ = f.Write(pmtPacket(0x101, streamTypeH264, 0x102))
	_, _ = f.Write(tsPacket(0x101, true, true, []byte{0, 0, 1, 0xe0, 0, 0, 0x80, 0, 0, 0, 0, 1, 0x65}))
	inner := f.Subscribe("rtsp")
	inner.Protect()
	player := f.Subscribe("player")
	f.DropAll()
	select {
	case <-player.Done():
	default:
		t.Fatal("a player must be dropped")
	}
	select {
	case <-inner.Done():
		t.Fatal("a protected reader must survive DropAll")
	default:
	}
	if f.Count() != 1 {
		t.Fatalf("count %d", f.Count())
	}
}
