package srt

import (
	"sync"
	"time"
)

// The ingest event log: the moments the FEED misbehaved, stamped where the
// bytes actually arrive — before any pipeline could be blamed. A feed gap
// here is ground truth for "the uplink (or sender) starved us at 21:37";
// its absence while viewers report incidents acquits the ingest entirely.
// Per room, long retention: the point is looking things up an hour after
// someone on the couch said "did you see that?".

// IngestEvent is one notable moment on a room's inbound feed.
type IngestEvent struct {
	Time   time.Time `json:"time"`
	Type   string    `json:"type"` // connect | disconnect | feed-gap | queue-drop
	DurMs  int64     `json:"durMs,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

const (
	eventsKeepPerChannel = 300
	eventsMaxAge         = 24 * time.Hour
	// feedGapAfter is how long a read may block before the silence counts
	// as a feed gap. Segment-sized pauses are normal on some containers;
	// one second of NOTHING on a live feed is not.
	feedGapAfter = time.Second
)

var (
	_eventsMu sync.Mutex
	_events   = map[string][]IngestEvent{}
)

func recordIngestEvent(channelID, eventType string, durMs int64, detail string) {
	_eventsMu.Lock()
	defer _eventsMu.Unlock()
	list := append(_events[channelID], IngestEvent{
		Time: time.Now(), Type: eventType, DurMs: durMs, Detail: detail,
	})
	cutoff := time.Now().Add(-eventsMaxAge)
	i := 0
	for i < len(list) && list[i].Time.Before(cutoff) {
		i++
	}
	list = list[i:]
	if len(list) > eventsKeepPerChannel {
		list = list[len(list)-eventsKeepPerChannel:]
	}
	_events[channelID] = list
}

// GetIngestEvents returns a room's recent feed events, oldest first.
func GetIngestEvents(channelID string) []IngestEvent {
	_eventsMu.Lock()
	defer _eventsMu.Unlock()
	return append([]IngestEvent{}, _events[channelID]...)
}
