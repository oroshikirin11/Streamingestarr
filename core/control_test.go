package core

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestEncodeControlCommand(t *testing.T) {
	data, err := encodeControlCommand(controlCommand{Type: "pause", Votes: 2, Viewers: 5, By: []string{"Alex", "Sam"}, ID: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"pause","votes":2,"viewers":5,"by":["Alex","Sam"],"id":"abc"}`
	if string(data) != want {
		t.Fatalf("got %s\nwant %s", data, want)
	}
	// by is never null.
	data, _ = encodeControlCommand(controlCommand{Type: "resume", ID: "x"})
	if string(data) != `{"type":"resume","votes":0,"viewers":0,"by":[],"id":"x"}` {
		t.Fatalf("empty by must encode as []: %s", data)
	}
	ping, _ := json.Marshal(controlPing{Type: "ping"})
	if string(ping) != `{"type":"ping"}` {
		t.Fatalf("ping = %s", ping)
	}
}

func TestDecodeControlFrame(t *testing.T) {
	ack, err := decodeControlFrame([]byte(`{"type":"ack","id":"abc","ok":false,"reason":"live source","paused":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if ack.Type != "ack" || ack.ID != "abc" || ack.OK || ack.Reason != "live source" || ack.Paused {
		t.Fatalf("ack decoded wrong: %+v", ack)
	}
	okAck, _ := decodeControlFrame([]byte(`{"type":"ack","id":"abc","ok":true,"paused":true}`))
	if !okAck.OK || !okAck.Paused {
		t.Fatalf("ok ack decoded wrong: %+v", okAck)
	}
	state, err := decodeControlFrame([]byte(`{"type":"state","paused":true,"pausedBy":"host","pending":"","pauseVote":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if state.Type != "state" || !state.Paused || state.PausedBy != "host" || state.Pending != "" || !state.PauseVote {
		t.Fatalf("state decoded wrong: %+v", state)
	}
	pong, _ := decodeControlFrame([]byte(`{"type":"pong"}`))
	if pong.Type != "pong" {
		t.Fatal("pong")
	}
	if _, err := decodeControlFrame([]byte(`{"paused":true}`)); err == nil {
		t.Fatal("a frame without a type must be rejected")
	}
	if _, err := decodeControlFrame([]byte(`nope`)); err == nil {
		t.Fatal("garbage must be rejected")
	}
}

func TestNewCommandID(t *testing.T) {
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a, b := newCommandID(), newCommandID()
	if !uuid.MatchString(a) || !uuid.MatchString(b) {
		t.Fatalf("not uuids: %s %s", a, b)
	}
	if a == b {
		t.Fatal("ids must differ")
	}
}
