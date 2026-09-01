package admin

import (
	"net/http"

	"streamingestarr/core"
	"streamingestarr/core/rtmp"
	"streamingestarr/core/srt"
	"streamingestarr/persistence/channelrepository"
	webutils "streamingestarr/webserver/utils"
)

// DisconnectInboundConnection force-disconnects a room's inbound stream —
// whichever protocol carries it. ?channel= picks the room, default room
// otherwise.
func DisconnectInboundConnection(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel")
	if channelID == "" || channelrepository.GetChannel(channelID) == nil {
		channelID = channelrepository.DefaultChannelID
	}

	channel := core.GetChannelRuntime(channelID)
	if channel == nil || !channel.GetStatus().Online {
		webutils.WriteSimpleResponse(w, false, "no inbound stream connected")
		return
	}

	rtmp.Disconnect(channelID)
	srt.Disconnect(channelID)
	webutils.WriteSimpleResponse(w, true, "inbound stream disconnected")
}
