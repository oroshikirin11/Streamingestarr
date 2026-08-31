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

	lastDropLog time.Time
}

// _queueDroppedBytes is the session's overflow counter, exposed in the
// admin ingest stats; reset at each publisher connect.
var _queueDroppedBytes atomic.Int64

func newPipeQueue(maxBytes int) *pipeQueue {
	q := &pipeQueue{max: maxBytes}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// push copies b into the queue. Never blocks: over budget, the oldest
// chunks go first and the loss is counted and logged (rate-limited).
func (q *pipeQueue) push(b []byte) {
	chunk := append([]byte(nil), b...)
	q.mu.Lock()
	q.chunks = append(q.chunks, chunk)
	q.bytes += len(chunk)
	var dropped int
	for q.bytes > q.max && len(q.chunks) > 1 {
		dropped += len(q.chunks[0])
		q.bytes -= len(q.chunks[0])
		q.chunks = q.chunks[1:]
	}
	if dropped > 0 {
		_queueDroppedBytes.Add(int64(dropped))
		if time.Since(q.lastDropLog) > 10*time.Second {
			q.lastDropLog = time.Now()
			log.Warnf("ingest buffer overflowed — dropped %dKB of the oldest stream data; the transcoder is not consuming fast enough", dropped/1024)
		}
	}
	q.mu.Unlock()
	q.cond.Signal()
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
	return chunk, true
}

func (q *pipeQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
}
