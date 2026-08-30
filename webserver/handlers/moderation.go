package handlers

import (
	"net/http"

	"streamingestarr/webserver/handlers/generated"
	"streamingestarr/webserver/handlers/moderation"
	"streamingestarr/webserver/router/middleware"
)

func (*ServerInterfaceImpl) GetUserDetails(w http.ResponseWriter, r *http.Request, userId string, params generated.GetUserDetailsParams) {
	middleware.RequireUserModerationScopeAccesstoken(moderation.GetUserDetails)(w, r)
}
