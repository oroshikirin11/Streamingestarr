package handlers

import (
	"net/http"

	"streamingestarr/webserver/handlers/admin"
	"streamingestarr/webserver/handlers/generated"
	"streamingestarr/webserver/router/middleware"
)

type ServerInterfaceImpl struct{}

// ensure ServerInterfaceImpl implements ServerInterface.
var _ generated.ServerInterface = &ServerInterfaceImpl{}

func New() *ServerInterfaceImpl {
	return &ServerInterfaceImpl{}
}

func (s *ServerInterfaceImpl) Handler() http.Handler {
	return generated.Handler(s)
}

func (*ServerInterfaceImpl) GetStatus(w http.ResponseWriter, r *http.Request) {
	GetStatus(w, r)
}

func (*ServerInterfaceImpl) GetCustomEmojiList(w http.ResponseWriter, r *http.Request) {
	GetCustomEmojiList(w, r)
}

func (*ServerInterfaceImpl) GetChatMessages(w http.ResponseWriter, r *http.Request, params generated.GetChatMessagesParams) {
	middleware.RequireUserAccessToken(GetChatMessages)(w, r)
}

func (*ServerInterfaceImpl) RegisterAnonymousChatUser(w http.ResponseWriter, r *http.Request, params generated.RegisterAnonymousChatUserParams) {
	RegisterAnonymousChatUser(w, r)
}

func (*ServerInterfaceImpl) RegisterAnonymousChatUserOptions(w http.ResponseWriter, r *http.Request) {
	RegisterAnonymousChatUser(w, r)
}

func (*ServerInterfaceImpl) UpdateMessageVisibility(w http.ResponseWriter, r *http.Request, params generated.UpdateMessageVisibilityParams) {
	middleware.RequireUserModerationScopeAccesstoken(admin.UpdateMessageVisibility)(w, r)
}

func (*ServerInterfaceImpl) UpdateUserEnabled(w http.ResponseWriter, r *http.Request, params generated.UpdateUserEnabledParams) {
	middleware.RequireUserModerationScopeAccesstoken(admin.UpdateUserEnabled)(w, r)
}

func (*ServerInterfaceImpl) GetWebConfig(w http.ResponseWriter, r *http.Request) {
	GetWebConfig(w, r)
}

func (*ServerInterfaceImpl) GetAllSocialPlatforms(w http.ResponseWriter, r *http.Request) {
	GetAllSocialPlatforms(w, r)
}

func (*ServerInterfaceImpl) GetVideoStreamOutputVariants(w http.ResponseWriter, r *http.Request) {
	GetVideoStreamOutputVariants(w, r)
}

func (*ServerInterfaceImpl) Ping(w http.ResponseWriter, r *http.Request) {
	Ping(w, r)
}

func (*ServerInterfaceImpl) ReportPlaybackMetrics(w http.ResponseWriter, r *http.Request) {
	ReportPlaybackMetrics(w, r)
}
