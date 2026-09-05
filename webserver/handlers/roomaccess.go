package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"streamingestarr/auth"
	"streamingestarr/core"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/utils"
)

// UnlockRoom takes a viewer's attempt at a room's own password:
// POST /api/rooms/unlock {"channel": "...", "password": "..."}.
// Throttled per IP like the door; success is remembered for the session.
func UnlockRoom(w http.ResponseWriter, r *http.Request) {
	ip := utils.GetIPAddressFromRequest(r)
	if wait := auth.ThrottleCheck(ip); wait > 0 {
		authJSON(w, http.StatusTooManyRequests, false, fmt.Sprintf("Too many attempts. Try again in %ds.", wait))
		return
	}
	var req struct {
		Channel  string `json:"channel"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		authJSON(w, http.StatusBadRequest, false, "Invalid request.")
		return
	}
	req.Channel = strings.TrimSpace(req.Channel)
	if req.Channel == "" {
		req.Channel = channelrepository.DefaultChannelID
	}
	if channelrepository.GetChannel(req.Channel) == nil {
		authJSON(w, http.StatusNotFound, false, "No such room.")
		return
	}
	if err := core.UnlockRoom(r, req.Channel, req.Password); err != nil {
		auth.ThrottleFail(ip)
		authJSON(w, http.StatusUnauthorized, false, err.Error())
		return
	}
	auth.ThrottleReset(ip)
	authJSON(w, http.StatusOK, true, "")
}
