package srt

import (
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

// pipeQueue decouples the SRT read loop from the transcoder's pipe.
//
// The pipe is synchronous: a Write blocks until ffmpeg reads. With the
// reader writing the pipe directly, any pause in ffmpeg's consumption (a
// segment flush, a busy CPU) stopped conn.Read entirely — and gosrt, being
// a LIVE protocol, dropped the packets that aged past the latency window
// while nobody was reading. Those holes reached the muxer as corrupt TS
// and painted intermittent artifacts that scaled with bitrate.
//
// The queue makes the reader unblockable: it always accepts (dropping the
// OLDEST data with a visible log once the budget is exhausted — a stall
// long enough to overflow ~10s of stream is a real incident, not something
// to hide), and a dedicated drain goroutine feeds ffmpeg at whatever pace
// it can take.
type pipeQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	chunks [][]byte
	bytes  int
	max    int
	closed bool
	// lossless flips overflow behavior: instead of dropping oldest, push
	// BLOCKS until the drain makes room. Correct for TCP — the kernel then
	// backpressures the sender, which retransmits nothing and loses
	// nothing; dropping here would reintroduce loss on the one transport
	// that guarantees losslessness. SRT stays drop-oldest: blocking its
	// reader is exactly the stall-starves-the-window bug this queue fixed.
	lossless bool

	// dropped is the owning session's overflow counter, exposed in the
	// admin ingest stats.
	dropped *atomic.Int64

	lastDropLog time.Time
}

func newPipeQueue(maxBytes int, lossless bool, dropped *atomic.Int64) *pipeQueue {
	q := &pipeQueue{max: maxBytes, lossless: lossless, dropped: dropped}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// push copies b into the queue. Lossy mode never blocks: over budget, the
// oldest chunks go first and the loss is counted and logged (rate-limited).
// Lossless mode blocks until the drain makes room (see the field comment).
func (q *pipeQueue) push(b []byte) {
	chunk := append([]byte(nil), b...)
	q.mu.Lock()
	if q.lossless {
		for q.bytes+len(chunk) > q.max && !q.closed {
			q.cond.Wait()
		}
		if q.closed {
			q.mu.Unlock()
			return
		}
	}
	q.chunks = append(q.chunks, chunk)
	q.bytes += len(chunk)
	var dropped int
	for q.bytes > q.max && len(q.chunks) > 1 {
		dropped += len(q.chunks[0])
		q.bytes -= len(q.chunks[0])
		q.chunks = q.chunks[1:]
	}
	if dropped > 0 {
		q.dropped.Add(int64(dropped))
		if time.Since(q.lastDropLog) > 10*time.Second {
			q.lastDropLog = time.Now()
			log.Warnf("ingest buffer overflowed — dropped %dKB of the oldest stream data; the transcoder is not consuming fast enough", dropped/1024)
		}
	}
	q.mu.Unlock()
	q.cond.Broadcast()
}

// pop blocks until a chunk is available; ok=false once closed and drained.
func (q *pipeQueue) pop() ([]byte, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.chunks) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.chunks) == 0 {
		return nil, false
	}
	chunk := q.chunks[0]
	q.chunks = q.chunks[1:]
	q.bytes -= len(chunk)
	// Wake a lossless pusher waiting for room (and fellow poppers; both
	// re-check their conditions).
	q.cond.Broadcast()
	return chunk, true
}

func (q *pipeQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
}
