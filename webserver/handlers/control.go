package handlers

import (
	"net/http"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"

	"streamingestarr/core"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/persistence/userrepository"
	webutils "streamingestarr/webserver/utils"
)

var controlUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// The sender is another service, not a browser page.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ControlConnection is the sender's control websocket for viewer pause
// votes: GET /api/integrations/control?accessToken=<token>&channel=<room>.
// A websocket cannot carry a bearer header, so the external-API token
// rides the query — same scope as the metadata push.
func ControlConnection(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("accessToken")
	if token == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	userRepository := userrepository.Get()
	integration, err := userRepository.GetExternalAPIUserForAccessTokenAndScope(token, models.ScopeCanSendSystemMessages)
	if integration == nil || err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	channelID := r.URL.Query().Get("channel")
	if channelID == "" {
		channelID = channelrepository.DefaultChannelID
	}
	channel := core.GetChannelRuntime(channelID)
	if channel == nil || channelrepository.GetChannel(channelID) == nil {
		webutils.WriteSimpleResponse(w, false, "unknown channel")
		return
	}

	conn, err := controlUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Debugln("control: upgrade failed:", err)
		return
	}
	if err := userRepository.SetExternalAPIUserAccessTokenAsUsed(token); err != nil {
		log.Debugln("token not found when updating last_used timestamp")
	}
	// Serves the connection until it ends.
	channel.AttachControl(conn)
}
