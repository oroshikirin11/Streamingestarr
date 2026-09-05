package relay

import (
	"crypto/rand"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mpegts"
	tscodecs "github.com/bluenviron/mediacommon/v2/pkg/formats/mpegts/codecs"
	"github.com/pion/rtp"
	log "github.com/sirupsen/logrus"
)

// The RTSP outlet: one server for every room, a publisher per live room
// that demuxes the fan-out's transport stream into RTP. Paths are
// /<room>/<token>; the resolver says which room a path opens.

// Resolver maps a request path to a room id, or "" when the path is not
// a valid relay link.
type Resolver func(path string) string

// RTSPServer serves every room's relay over RTSP.
type RTSPServer struct {
	srv     *gortsplib.Server
	resolve Resolver

	mu         sync.RWMutex
	publishers map[string]*Publisher               // room → live publisher
	sessions   map[*gortsplib.ServerSession]string // playing session → room
}

// StartRTSP listens on the port; TCP only — the interleaved transport
// AVPro asks for with rtspt://, and the one that crosses NAT.
func StartRTSP(port int, resolve Resolver) (*RTSPServer, error) {
	s := &RTSPServer{resolve: resolve, publishers: map[string]*Publisher{}, sessions: map[*gortsplib.ServerSession]string{}}
	s.srv = &gortsplib.Server{
		Handler:     s,
		RTSPAddress: fmt.Sprintf(":%d", port),
	}
	if err := s.srv.Start(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close stops the server.
func (s *RTSPServer) Close() {
	s.srv.Close()
}

// Publish starts serving a room from its fan-out. Idempotent per room:
// a second call replaces the publisher.
func (s *RTSPServer) Publish(room string, fan *Fanout) {
	p := &Publisher{room: room, fan: fan, server: s.srv}
	s.mu.Lock()
	if old := s.publishers[room]; old != nil {
		old.close()
	}
	s.publishers[room] = p
	s.mu.Unlock()
	go p.run()
}

// Unpublish stops serving a room; its players are dropped.
func (s *RTSPServer) Unpublish(room string) {
	s.mu.Lock()
	p := s.publishers[room]
	delete(s.publishers, room)
	s.mu.Unlock()
	if p != nil {
		p.close()
	}
	s.DropPlayers(room)
}

// Players is how many sessions are playing a room.
func (s *RTSPServer) Players(room string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, r := range s.sessions {
		if r == room {
			n++
		}
	}
	return n
}

// DropPlayers closes every session playing a room.
func (s *RTSPServer) DropPlayers(room string) {
	s.mu.Lock()
	var drop []*gortsplib.ServerSession
	for ss, r := range s.sessions {
		if r == room {
			drop = append(drop, ss)
		}
	}
	s.mu.Unlock()
	for _, ss := range drop {
		ss.Close()
	}
}

func (s *RTSPServer) streamFor(path string) *gortsplib.ServerStream {
	room := s.resolve(path)
	if room == "" {
		return nil
	}
	s.mu.RLock()
	p := s.publishers[room]
	s.mu.RUnlock()
	if p == nil {
		return nil
	}
	return p.serverStream()
}

// OnConnOpen implements gortsplib.ServerHandler.
func (s *RTSPServer) OnConnOpen(_ *gortsplib.ServerHandlerOnConnOpenCtx) {}

// OnConnClose implements gortsplib.ServerHandler.
func (s *RTSPServer) OnConnClose(_ *gortsplib.ServerHandlerOnConnCloseCtx) {}

// OnSessionOpen implements gortsplib.ServerHandler.
func (s *RTSPServer) OnSessionOpen(_ *gortsplib.ServerHandlerOnSessionOpenCtx) {}

// OnSessionClose implements gortsplib.ServerHandler.
func (s *RTSPServer) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {
	s.mu.Lock()
	delete(s.sessions, ctx.Session)
	s.mu.Unlock()
}

// OnDescribe implements gortsplib.ServerHandler.
func (s *RTSPServer) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	st := s.streamFor(ctx.Path)
	if st == nil {
		return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
	}
	return &base.Response{StatusCode: base.StatusOK}, st, nil
}

// OnSetup implements gortsplib.ServerHandler.
func (s *RTSPServer) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	st := s.streamFor(ctx.Path)
	if st == nil {
		return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
	}
	return &base.Response{StatusCode: base.StatusOK}, st, nil
}

// OnPlay implements gortsplib.ServerHandler.
func (s *RTSPServer) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	room := s.resolve(ctx.Path)
	if room == "" {
		return &base.Response{StatusCode: base.StatusNotFound}, nil
	}
	s.mu.Lock()
	s.sessions[ctx.Session] = room
	s.mu.Unlock()
	return &base.Response{StatusCode: base.StatusOK}, nil
}

// Publisher turns one room's transport stream into an RTSP stream.
type Publisher struct {
	room   string
	fan    *Fanout
	server *gortsplib.Server

	mu     sync.RWMutex
	stream *gortsplib.ServerStream
	client *Client
	closed bool
}

func (p *Publisher) serverStream() *gortsplib.ServerStream {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stream
}

func (p *Publisher) close() {
	p.mu.Lock()
	p.closed = true
	if p.client != nil {
		p.client.Close()
	}
	if p.stream != nil {
		p.stream.Close()
		p.stream = nil
	}
	p.mu.Unlock()
}

func randUint32() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// run reads the fan-out through a pipe into the demuxer and forwards
// every access unit as RTP. It ends with the stream.
func (p *Publisher) run() {
	client := p.fan.Subscribe("rtsp")
	client.Protect()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		client.Close()
		return
	}
	p.client = client
	p.mu.Unlock()

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for {
			select {
			case chunk := <-client.Packets():
				if _, err := pw.Write(chunk); err != nil {
					return
				}
			case <-client.Done():
				// Drain what is queued, then end.
				for {
					select {
					case chunk := <-client.Packets():
						if _, err := pw.Write(chunk); err != nil {
							return
						}
					default:
						return
					}
				}
			}
		}
	}()
	defer pr.Close()

	reader := &mpegts.Reader{R: pr}
	if err := reader.Initialize(); err != nil {
		log.Debugf("relay rtsp %s: no tables: %v", p.room, err)
		return
	}
	reader.OnDecodeError(func(err error) { log.Debugf("relay rtsp %s: %v", p.room, err) })

	desc := &description.Session{}
	var videoMedia, audioMedia *description.Media
	var h264 *format.H264
	var h265 *format.H265
	var aac *format.MPEG4Audio
	var videoTrack, audioTrack *mpegts.Track
	for _, track := range reader.Tracks() {
		switch c := track.Codec.(type) {
		case *tscodecs.H264:
			if videoMedia != nil {
				continue
			}
			h264 = &format.H264{PayloadTyp: 96, PacketizationMode: 1}
			videoMedia = &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{h264}}
			videoTrack = track
		case *tscodecs.H265:
			if videoMedia != nil {
				continue
			}
			h265 = &format.H265{PayloadTyp: 96}
			videoMedia = &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{h265}}
			videoTrack = track
		case *tscodecs.MPEG4Audio:
			if audioMedia != nil {
				continue
			}
			cfg := c.Config
			aac = &format.MPEG4Audio{PayloadTyp: 97, Config: &cfg, SizeLength: 13, IndexLength: 3, IndexDeltaLength: 3}
			audioMedia = &description.Media{Type: description.MediaTypeAudio, Formats: []format.Format{aac}}
			audioTrack = track
		}
	}
	if videoMedia == nil {
		log.Warnf("relay rtsp %s: no H.264 or HEVC video in the stream, nothing to serve", p.room)
		return
	}
	desc.Medias = append(desc.Medias, videoMedia)
	if audioMedia != nil {
		desc.Medias = append(desc.Medias, audioMedia)
	}

	stream := &gortsplib.ServerStream{Server: p.server, Desc: desc}
	if err := stream.Initialize(); err != nil {
		log.Errorf("relay rtsp %s: %v", p.room, err)
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		stream.Close()
		return
	}
	p.stream = stream
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if p.stream == stream {
			p.stream = nil
		}
		p.mu.Unlock()
		stream.Close()
	}()

	videoStart := randUint32()
	audioStart := randUint32()
	timeDec := mpegts.TimeDecoder{}
	timeDec.Initialize()

	if h264 != nil {
		enc, err := h264.CreateEncoder()
		if err != nil {
			log.Errorf("relay rtsp %s: %v", p.room, err)
			return
		}
		reader.OnDataH264(videoTrack, func(pts, _ int64, au [][]byte) error {
			var sps, pps []byte
			for _, nalu := range au {
				if len(nalu) == 0 {
					continue
				}
				switch nalu[0] & 0x1f {
				case 7:
					sps = nalu
				case 8:
					pps = nalu
				}
			}
			if sps != nil && pps != nil {
				h264.SafeSetParams(sps, pps)
			}
			pkts, err := enc.Encode(au)
			if err != nil {
				return nil // a malformed unit is skipped, not fatal
			}
			ts := videoStart + uint32(timeDec.Decode(pts))
			return writeAll(stream, videoMedia, pkts, ts)
		})
	}
	if h265 != nil {
		enc, err := h265.CreateEncoder()
		if err != nil {
			log.Errorf("relay rtsp %s: %v", p.room, err)
			return
		}
		reader.OnDataH265(videoTrack, func(pts, _ int64, au [][]byte) error {
			var vps, sps, pps []byte
			for _, nalu := range au {
				if len(nalu) == 0 {
					continue
				}
				switch (nalu[0] >> 1) & 0x3f {
				case 32:
					vps = nalu
				case 33:
					sps = nalu
				case 34:
					pps = nalu
				}
			}
			if vps != nil && sps != nil && pps != nil {
				h265.SafeSetParams(vps, sps, pps)
			}
			pkts, err := enc.Encode(au)
			if err != nil {
				return nil
			}
			ts := videoStart + uint32(timeDec.Decode(pts))
			return writeAll(stream, videoMedia, pkts, ts)
		})
	}
	if aac != nil {
		enc, err := aac.CreateEncoder()
		if err != nil {
			log.Errorf("relay rtsp %s: %v", p.room, err)
			return
		}
		rate := int64(aac.ClockRate())
		reader.OnDataMPEG4Audio(audioTrack, func(pts int64, aus [][]byte) error {
			pkts, err := enc.Encode(aus)
			if err != nil {
				return nil
			}
			// MPEG-TS clocks at 90 kHz; the RTP clock for AAC is its sample rate.
			ts := audioStart + uint32(timeDec.Decode(pts)*rate/90000)
			return writeAll(stream, audioMedia, pkts, ts)
		})
	}

	for {
		if err := reader.Read(); err != nil {
			if err != io.EOF && !strings.Contains(err.Error(), "closed pipe") {
				log.Debugf("relay rtsp %s: reader ended: %v", p.room, err)
			}
			return
		}
	}
}

// writeAll stamps and sends one access unit's packets. The encoder's own
// timestamps are relative; the stream's are absolute per track.
func writeAll(stream *gortsplib.ServerStream, media *description.Media, pkts []*rtp.Packet, ts uint32) error {
	for _, pkt := range pkts {
		pkt.Timestamp = ts
		if err := stream.WritePacketRTP(media, pkt); err != nil {
			return err
		}
	}
	return nil
}
