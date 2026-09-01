package srt

import (
	"io"
	"sync"
	"sync/atomic"

	"streamingestarr/persistence/channelrepository"
)

// ingestSession is the live state of ONE channel's inbound stream. Every
// piece of it used to be a package singleton; rooms broadcast concurrently
// now, so each session carries its own connection, pipe, bitrate ring and
// overflow counter, keyed by the channel its stream key routed to.
type ingestSession struct {
	channelID string
	transport string
	conn      io.Closer
	pipe      *io.PipeWriter // set once the transcoder pipe exists

	brMu      sync.Mutex
	brSamples []BitrateSample

	queueDroppedBytes atomic.Int64
	// lastReadUnixMs is stamped by every successful socket read; "now
	// minus this" being large means the feed is silent RIGHT NOW.
	lastReadUnixMs atomic.Int64
}

var (
	_sessionsMu sync.Mutex
	_sessions   = map[string]*ingestSession{}
	// _lastSessions retains each channel's most recent finished session so
	// the admin graphs keep showing the numbers after a disconnect — the
	// same continuity the old package-level ring provided — until the next
	// broadcast replaces them.
	_lastSessions = map[string]*ingestSession{}
)

// channelIDForStreamKey resolves which channel a validated stream key
// feeds: a room's own key routes to that room, anything else (the global
// key list) to the default channel. Mirrors core.ChannelRuntimeForStreamKey,
// which this package cannot import.
func channelIDForStreamKey(key string) string {
	if id := channelrepository.GetChannelIDForKey(key); id != "" {
		return id
	}
	return channelrepository.DefaultChannelID
}

// registerSession claims the channel's ingest slot. nil means the slot is
// already taken — two publishers raced the busy check; the loser backs off.
func registerSession(channelID, transport string, conn io.Closer) *ingestSession {
	_sessionsMu.Lock()
	defer _sessionsMu.Unlock()
	if _, exists := _sessions[channelID]; exists {
		return nil
	}
	s := &ingestSession{channelID: channelID, transport: transport, conn: conn}
	_sessions[channelID] = s
	return s
}

// setPipe attaches the transcoder pipe once it exists, under the same lock
// Disconnect reads it with.
func (s *ingestSession) setPipe(pipe *io.PipeWriter) {
	_sessionsMu.Lock()
	s.pipe = pipe
	_sessionsMu.Unlock()
}

// unregisterSession frees the channel's slot, keeping the finished session
// around for the admin's post-disconnect stats.
func unregisterSession(s *ingestSession) {
	_sessionsMu.Lock()
	if _sessions[s.channelID] == s {
		delete(_sessions, s.channelID)
		_lastSessions[s.channelID] = s
	}
	_sessionsMu.Unlock()
}
