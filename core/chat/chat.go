package chat

import (
	"errors"
	"net/http"
	"sort"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	log "github.com/sirupsen/logrus"
	"streamingestarr/config"
	"streamingestarr/core/chat/events"
	"streamingestarr/models"
	"streamingestarr/persistence/chatmessagerepository"
	"streamingestarr/persistence/configrepository"
)

var (
	// getChannelStatus reports one room's stream status — chat behavior
	// (welcome messages, the offline gate) follows the room a client sits
	// in, not a global notion of "the stream".
	getChannelStatus        func(channelID string) models.Status
	chatMessagesSentCounter prometheus.Gauge
)

// Start begins the chat server.
func Start(getChannelStatusFunc func(channelID string) models.Status) error {
	setupPersistence()

	configRepository := configrepository.Get()

	getChannelStatus = getChannelStatusFunc
	_server = NewChat()

	go _server.Run()

	log.Traceln("Chat server started with max connection count of", _server.maxSocketConnectionLimit)

	chatMessagesSentCounter = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "total_chat_message_count",
		Help: "The number of chat messages incremented over time.",
		ConstLabels: map[string]string{
			"version": config.VersionNumber,
			"host":    configRepository.GetServerURL(),
		},
	})

	return nil
}

// Pause-vote hooks. The vote state lives in core (alongside the room
// runtime); chat only carries the messages and reports who is seated.
// core sets these at start — chat cannot import core.
var (
	// pauseVoteHandler receives a viewer's PAUSE_VOTE: action is
	// "pause", "resume" or "withdraw".
	pauseVoteHandler func(channelID string, user *models.User, action string)
	// presenceChangedHandler fires when a room's distinct-user count may
	// have changed — a join or the last connection of a user closing.
	// clientID is the joining connection (0 on leave) so the room can
	// hand it the current tally even when nothing changed.
	presenceChangedHandler func(channelID string, clientID uint)
)

// roomAccessCheck says whether a request may take a seat in a room —
// the room's mode and its own password, decided by core.
var roomAccessCheck func(r *http.Request, channelID string) bool

// SetRoomAccessCheck wires the room access decision in.
func SetRoomAccessCheck(check func(r *http.Request, channelID string) bool) {
	roomAccessCheck = check
}

// SetPauseVoteHooks wires the room-side vote logic in.
func SetPauseVoteHooks(onVote func(channelID string, user *models.User, action string), onPresence func(channelID string, clientID uint)) {
	pauseVoteHandler = onVote
	presenceChangedHandler = onPresence
}

// DistinctUserCount is how many different users hold at least one
// connection in a room — the people who can press the vote button.
func DistinctUserCount(channelID string) int {
	if _server == nil {
		return 0
	}
	_server.mu.RLock()
	defer _server.mu.RUnlock()
	seen := map[string]struct{}{}
	for _, client := range _server.clients {
		if client == nil || client.ChannelID != channelID || client.User == nil {
			continue
		}
		seen[client.User.ID] = struct{}{}
	}
	return len(seen)
}

// UserSeated reports whether a user still holds a connection in a room.
func UserSeated(channelID, userID string) bool {
	if _server == nil {
		return false
	}
	_server.mu.RLock()
	defer _server.mu.RUnlock()
	for _, client := range _server.clients {
		if client != nil && client.ChannelID == channelID && client.User != nil && client.User.ID == userID {
			return true
		}
	}
	return false
}

// SendPayloadToClient delivers one payload to one connection.
func SendPayloadToClient(clientID uint, payload events.EventPayload) {
	if _server == nil {
		return
	}
	_server.mu.RLock()
	client, ok := _server.clients[clientID]
	_server.mu.RUnlock()
	if ok && client != nil {
		client.sendPayload(payload)
	}
}

// SendPayloadToChannel delivers a raw payload to everyone in a room.
func SendPayloadToChannel(channelID string, payload events.EventPayload) {
	if _server == nil {
		return
	}
	_ = _server.BroadcastToChannel(channelID, payload)
}

// GetClientsForUser will return chat connections that are owned by a specific user.
func GetClientsForUser(userID string) ([]*Client, error) {
	_server.mu.Lock()
	defer _server.mu.Unlock()

	clients := map[string][]*Client{}

	for _, client := range _server.clients {
		clients[client.User.ID] = append(clients[client.User.ID], client)
	}

	if _, exists := clients[userID]; !exists {
		return nil, errors.New("no connections for user found")
	}

	return clients[userID], nil
}

// FindClientByID will return a single connected client by ID.
func FindClientByID(clientID uint) (*Client, bool) {
	client, found := _server.clients[clientID]
	return client, found
}

// GetClients will return all the current chat clients connected.
func GetClients() []*Client {
	clients := []*Client{}

	if _server == nil {
		return clients
	}

	// Convert the keyed map to a slice.
	for _, client := range _server.clients {
		clients = append(clients, client)
	}

	sort.Slice(clients, func(i, j int) bool {
		return clients[i].ConnectedAt.Before(clients[j].ConnectedAt)
	})

	return clients
}

// SendSystemMessage sends a message string as a system message to one
// room's clients — or, with channelID "", to everyone in every room.
func SendSystemMessage(channelID string, text string, ephemeral bool) error {
	message := events.SystemMessageEvent{
		MessageEvent: events.MessageEvent{
			Body: text,
		},
	}
	message.SetDefaults()
	message.RenderBody()

	if err := BroadcastToChannel(channelID, &message); err != nil {
		log.Errorln("error sending system message", err)
	}

	if !ephemeral {
		chatMessageRepository := chatmessagerepository.Get()
		chatMessageRepository.SaveEvent(message.ID, nil, message.Body, message.GetMessageType(), nil, message.Timestamp, nil, nil, nil, nil, channelID)
	}

	return nil
}

// SendSystemAction sends a system action string as an action event to one
// room's clients — or, with channelID "", to everyone in every room.
func SendSystemAction(channelID string, text string, ephemeral bool) error {
	message := events.ActionEvent{
		MessageEvent: events.MessageEvent{
			Body: text,
		},
	}

	message.SetDefaults()
	message.RenderBody()

	if err := BroadcastToChannel(channelID, &message); err != nil {
		log.Errorln("error sending system chat action")
	}

	if !ephemeral {
		chatMessageRepository := chatmessagerepository.Get()
		chatMessageRepository.SaveEvent(message.ID, nil, message.Body, message.GetMessageType(), nil, message.Timestamp, nil, nil, nil, nil, channelID)
	}

	return nil
}

// SendAllWelcomeMessage sends the welcome message to a room's clients.
func SendAllWelcomeMessage(channelID string) {
	_server.sendAllWelcomeMessage(channelID)
}

// SendSystemMessageToClient will send a single message to a single connected chat client.
func SendSystemMessageToClient(clientID uint, text string) {
	if client, foundClient := FindClientByID(clientID); foundClient {
		_server.sendSystemMessageToClient(client, text)
	}
}

// Broadcast will send all connected clients the outbound object provided.
func Broadcast(event events.OutboundEvent) error {
	return _server.Broadcast(event.GetBroadcastPayload())
}

// BroadcastToChannel sends the outbound object to one room's clients; an
// empty channelID falls back to everyone (global admin actions).
func BroadcastToChannel(channelID string, event events.OutboundEvent) error {
	if channelID == "" {
		return _server.Broadcast(event.GetBroadcastPayload())
	}
	return _server.BroadcastToChannel(channelID, event.GetBroadcastPayload())
}

// HandleClientConnection handles a single inbound websocket connection.
func HandleClientConnection(w http.ResponseWriter, r *http.Request) {
	_server.HandleClientConnection(w, r)
}

// DisconnectClients will forcefully disconnect all clients belonging to a user by ID.
func DisconnectClients(clients []*Client) {
	_server.DisconnectClients(clients)
}
