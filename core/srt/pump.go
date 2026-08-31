// The transport-agnostic half of every ingest: everything that happens
// after a publisher is authenticated, from head sniff to teardown. SRT and
// TCP both land here — the pipeline never cared what carried the bytes.
package srt

import (
	"io"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"streamingestarr/config"
	"streamingestarr/models"
)

// ingestConn is what pump needs from a transport: reads with deadlines,
// and a Close that unblocks a stuck reader. gosrt.Conn and net.TCPConn
// both satisfy it as-is.
type ingestConn interface {
	io.ReadCloser
	SetReadDeadline(t time.Time) error
}

func pump(conn ingestConn, remoteAddr, transport, key string) {
	log.Infoln("Inbound", transport, "stream connected from", remoteAddr)

	broadcaster := models.Broadcaster{
		RemoteAddr: remoteAddr,
		Time:       time.Now(),
		StreamDetails: models.InboundStreamDetails{
			Encoder: transport,
		},
	}
	_setBroadcaster(broadcaster, key)

	// Sniff the audio codec BEFORE the transcoder spawns: its fMP4 path
	// picks a bitstream filter per audio codec, and picking wrong kills the
	// session at startup. Bounded — whatever arrives inside a second, capped
	// well under the details capture — and a failed sniff just leaves the
	// codec unknown, which the transcoder treats as it always has. The head
	// is not lost: it seeds both the pipe and the details capture below.
	const headMax = 256 << 10
	head := make([]byte, 0, headMax)
	{
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		hb := make([]byte, 1316)
		for len(head) < headMax {
			n, err := conn.Read(hb)
			if n > 0 {
				head = append(head, hb[:n]...)
			}
			if err != nil {
				break
			}
		}
		_ = conn.SetReadDeadline(time.Time{})
	}
	videoCodec, audioCodec := probeHeadCodecs(head)
	config.SetInboundVideoCodec(videoCodec)
	config.SetInboundAudioCodec(audioCodec)

	pipeOut, pipeIn := io.Pipe()

	_mu.Lock()
	_activeConn = conn
	_activePipe = pipeIn
	_mu.Unlock()

	_setStreamAsConnected(pipeOut, key)

	// The transcoder consumes the raw container from the pipe; ffmpeg probes
	// it itself, so no demuxing happens here. A copy of the stream does get
	// kept aside for ffprobe runs that fill in the broadcaster details the
	// admin shows — enough bytes for parameter sets, frames and a usable
	// bitrate window at 4K rates, capped by time so a thin stream still
	// qualifies. A fresh capture every 30s keeps the shown numbers tracking
	// a VBR source instead of freezing the connect-time estimate — but only
	// for mpegts, which self-syncs from any byte. A matroska/NUT chunk cut
	// mid-stream has no header to parse from, so those streams keep their
	// connect-time measurement.
	const probeTarget = 4 << 20
	const probeFloor = 128 << 10
	// 60s, and nice'd (probeCommand): the re-probe shares a small CPU with
	// the live transcoder and must stay a background citizen.
	const reprobeAfter = 60 * time.Second
	probeBuf := make([]byte, 0, probeTarget)
	captureStart := time.Now()
	var resumeAt time.Time // zero while a capture is filling
	var reprobe atomic.Bool
	reprobe.Store(true)

	// The reader must NEVER block on the transcoder (see pipeQueue): all
	// pipe writes happen on a drain goroutine, the reader only enqueues.
	// ~10s of stream at 25 Mbps fits the budget; a stall longer than that
	// drops oldest data audibly in the log instead of silently on the wire.
	_queueDroppedBytes.Store(0)
	q := newPipeQueue(32<<20, transport == "TCP")
	go func() {
		for {
			chunk, ok := q.pop()
			if !ok {
				return
			}
			if _, werr := pipeIn.Write(chunk); werr != nil {
				// The transcoder is gone; unblock the reader so the
				// session tears down instead of buffering into the void.
				_ = conn.Close()
				return
			}
		}
	}()

	if len(head) > 0 {
		q.push(head)
		probeBuf = append(probeBuf, head...)
	}

	// Live inbound rate: counted here where the bytes actually arrive,
	// sampled on the read loop's own clock — no goroutine to race.
	resetBitrate()
	brBytes := int64(len(head))
	brLast := time.Now()

	// The richest details seen this session; fresh probes merge into it
	// rather than replace it (a keyframe-less window must not blank the
	// card). Guarded because probe goroutines outlive their 30s slots.
	var detailsMu sync.Mutex
	lastDetails := models.InboundStreamDetails{Encoder: transport}

	buffer := make([]byte, 1316) // 7 mpegts packets, the SRT payload convention
	for {
		n, err := conn.Read(buffer)
		if n > 0 {
			q.push(buffer[:n])
			brBytes += int64(n)
			if since := time.Since(brLast); since >= bitrateSampleEvery {
				recordBitrate(brBytes, since)
				brBytes = 0
				brLast = time.Now()
			}
			if resumeAt.IsZero() {
				if room := probeTarget - len(probeBuf); room > 0 {
					probeBuf = append(probeBuf, buffer[:min(n, room)]...)
				}
				if len(probeBuf) >= probeTarget ||
					(len(probeBuf) >= probeFloor && time.Since(captureStart) > 3*time.Second) {
					prefix := probeBuf
					probeBuf = make([]byte, 0, probeTarget)
					resumeAt = time.Now().Add(reprobeAfter)
					go func() {
						if details, container := probeInboundStream(prefix, transport); container != "" {
							detailsMu.Lock()
							lastDetails = mergeDetails(lastDetails, details)
							b := broadcaster
							b.StreamDetails = lastDetails
							detailsMu.Unlock()
							_setBroadcaster(b, key)
							if container != "mpegts" {
								reprobe.Store(false)
							}
						}
					}()
				}
			} else if reprobe.Load() && time.Now().After(resumeAt) {
				resumeAt = time.Time{}
				captureStart = time.Now()
			}
		}
		if err != nil {
			break
		}
	}

	log.Infoln("Inbound", transport, "stream disconnected.")
	q.close()
	teardown(conn, pipeIn)
}

func teardown(conn io.Closer, pipe *io.PipeWriter) {
	_mu.Lock()
	defer _mu.Unlock()
	_ = conn.Close()
	_ = pipe.Close()
	if _activeConn == conn {
		_activeConn = nil
		_activePipe = nil
	}
	// The sniffed codecs belong to the session that just ended; a following
	// RTMP broadcast must not inherit them.
	config.SetInboundAudioCodec("")
	config.SetInboundVideoCodec("")
}

// Disconnect will force disconnect the current inbound stream connection.
func Disconnect() {
	_mu.Lock()
	conn, pipe := _activeConn, _activePipe
	_activeConn = nil
	_activePipe = nil
	_mu.Unlock()
	if conn == nil {
		return
	}
	log.Traceln("Inbound stream disconnect requested.")
	_ = conn.Close()
	if pipe != nil {
		_ = pipe.Close()
	}
}
