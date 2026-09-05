package core

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

// The control channel: receiver → sender, one websocket per room, over
// which viewer pause votes become commands. The sender (Streamerr)
// opens GET /api/integrations/control?accessToken=…&channel=<room>; the
// receiver sends {"type":"pause"|"resume",…} and reads acks and state
// frames back. Both sides ping every 20 s and drop after 60 s of silence.

const (
	controlPingPeriod = 20 * time.Second
	controlReadWait   = 60 * time.Second
	controlWriteWait  = 10 * time.Second
)

// controlCommand is what the receiver sends: a pause or resume the room
// voted for.
type controlCommand struct {
	Type    string   `json:"type"`
	Votes   int      `json:"votes"`
	Viewers int      `json:"viewers"`
	By      []string `json:"by"`
	ID      string   `json:"id"`
}

// controlFrame is anything the sender sends: ack, state, ping, pong.
type controlFrame struct {
	Type string `json:"type"`
	// ack
	ID     string `json:"id,omitempty"`
	OK     bool   `json:"ok,omitempty"`
	Reason string `json:"reason,omitempty"`
	// ack and state
	Paused bool `json:"paused,omitempty"`
	// state
	PausedBy  string `json:"pausedBy,omitempty"`
	Pending   string `json:"pending,omitempty"`
	PauseVote bool   `json:"pauseVote,omitempty"`
}

// controlPing is the keepalive in either direction.
type controlPing struct {
	Type string `json:"type"`
}

// encodeControlCommand renders a command as the JSON text frame the
// contract fixes; by is never null.
func encodeControlCommand(cmd controlCommand) ([]byte, error) {
	if cmd.By == nil {
		cmd.By = []string{}
	}
	return json.Marshal(cmd)
}

// decodeControlFrame parses one text frame from the sender.
func decodeControlFrame(data []byte) (controlFrame, error) {
	var frame controlFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return controlFrame{}, err
	}
	if frame.Type == "" {
		return controlFrame{}, fmt.Errorf("control frame without a type")
	}
	return frame, nil
}

// newCommandID mints a random UUID (v4 layout) for a command.
func newCommandID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// controlConn is one sender connection.
type controlConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	done    chan struct{}
	once    sync.Once
}

func (cc *controlConn) send(v interface{}) error {
	if cc == nil {
		return fmt.Errorf("no control connection")
	}
	var data []byte
	var err error
	if cmd, ok := v.(controlCommand); ok {
		data, err = encodeControlCommand(cmd)
	} else {
		data, err = json.Marshal(v)
	}
	if err != nil {
		return err
	}
	cc.writeMu.Lock()
	defer cc.writeMu.Unlock()
	_ = cc.conn.SetWriteDeadline(time.Now().Add(controlWriteWait))
	return cc.conn.WriteMessage(websocket.TextMessage, data)
}

func (cc *controlConn) close() {
	cc.once.Do(func() {
		close(cc.done)
		_ = cc.conn.Close()
	})
}

// AttachControl adopts a sender's control websocket for this room and
// serves it until it ends. A newer connection replaces the older one.
func (c *ChannelRuntime) AttachControl(conn *websocket.Conn) {
	cc := &controlConn{conn: conn, done: make(chan struct{})}

	pv := &c.pauseVote
	pv.mu.Lock()
	old := pv.control
	pv.control = cc
	pv.mu.Unlock()
	if old != nil {
		log.Debugln("control: replacing the sender connection of room", c.ID)
		old.close()
	}
	log.Infoln("control: sender connected to room", c.ID)
	c.recomputePauseVote(0)

	go func() {
		ticker := time.NewTicker(controlPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-cc.done:
				return
			case <-ticker.C:
				if err := cc.send(controlPing{Type: "ping"}); err != nil {
					cc.close()
					return
				}
			}
		}
	}()

	_ = conn.SetReadDeadline(time.Now().Add(controlReadWait))
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		_ = conn.SetReadDeadline(time.Now().Add(controlReadWait))
		frame, err := decodeControlFrame(data)
		if err != nil {
			log.Debugln("control: bad frame from the sender:", err)
			continue
		}
		switch frame.Type {
		case "ping":
			_ = cc.send(controlPing{Type: "pong"})
		case "pong":
		case "ack":
			c.handleControlAck(frame)
		case "state":
			c.handleControlState(frame)
		default:
			log.Debugln("control: unknown frame type", frame.Type)
		}
	}
	cc.close()

	pv.mu.Lock()
	if pv.control == cc {
		pv.control = nil
	}
	pv.mu.Unlock()
	log.Infoln("control: sender disconnected from room", c.ID)
	c.recomputePauseVote(0)
}
