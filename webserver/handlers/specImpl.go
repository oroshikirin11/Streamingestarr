package handlers

import (
	"net/http"

	"streamingestarr/models"
	"streamingestarr/webserver/router/middleware"
)

// Implementations for the endpoints documented in openapi.yaml that were
// historically registered as direct routes. The direct registrations for
// /api/* paths are gone; these route through the generated API handler.

func (*ServerInterfaceImpl) AuthLogin(w http.ResponseWriter, r *http.Request) {
	PostAuthLogin(w, r)
}

func (*ServerInterfaceImpl) AuthSetup(w http.ResponseWriter, r *http.Request) {
	PostAuthSetup(w, r)
}

func (*ServerInterfaceImpl) AuthLogout(w http.ResponseWriter, r *http.Request) {
	PostAuthLogout(w, r)
}

func (*ServerInterfaceImpl) AuthStatus(w http.ResponseWriter, r *http.Request) {
	GetAuthStatus(w, r)
}

func (*ServerInterfaceImpl) SetRoomPassword(w http.ResponseWriter, r *http.Request) {
	middleware.RequireAdminAuth(SetViewerLogin)(w, r)
}

func (*ServerInterfaceImpl) SetChatNameReservationDays(w http.ResponseWriter, r *http.Request) {
	middleware.RequireAdminAuth(SetChatNameReservationDays)(w, r)
}

func (*ServerInterfaceImpl) SetVideoSegmentFormat(w http.ResponseWriter, r *http.Request) {
	middleware.RequireAdminAuth(SetVideoSegmentFormat)(w, r)
}

func (*ServerInterfaceImpl) SetSRTEnabled(w http.ResponseWriter, r *http.Request) {
	middleware.RequireAdminAuth(SetSRTEnabled)(w, r)
}

func (*ServerInterfaceImpl) SetSRTPort(w http.ResponseWriter, r *http.Request) {
	middleware.RequireAdminAuth(SetSRTPort)(w, r)
}

func (*ServerInterfaceImpl) SetNowPlayingMetadata(w http.ResponseWriter, r *http.Request) {
	middleware.RequireExternalAPIAccessToken(models.ScopeCanSendSystemMessages, SetNowPlayingMetadata)(w, r)
}

func (*ServerInterfaceImpl) SetScheduleMetadata(w http.ResponseWriter, r *http.Request) {
	middleware.RequireExternalAPIAccessToken(models.ScopeCanSendSystemMessages, SetScheduleMetadata)(w, r)
}

func (*ServerInterfaceImpl) SetArtworkMetadata(w http.ResponseWriter, r *http.Request) {
	middleware.RequireExternalAPIAccessToken(models.ScopeCanSendSystemMessages, SetArtworkMetadata)(w, r)
}

func (*ServerInterfaceImpl) GetIntegrationCapabilities(w http.ResponseWriter, r *http.Request) {
	middleware.RequireExternalAPIAccessToken(models.ScopeCanSendSystemMessages, GetCapabilities)(w, r)
}
