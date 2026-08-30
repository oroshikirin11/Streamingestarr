package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"streamingestarr/auth"
	"streamingestarr/persistence/configrepository"
)

// The whole service sits behind the viewer gate — including HLS playlists
// and segments and the chat websocket, which Owncast served publicly.
// Routes on this list carry their own authentication (admin basic auth,
// integration bearer tokens) or must be reachable to log in at all.
var gateExemptPrefixes = []string{
	"/login",
	"/setup",
	"/api/auth/",
	"/api/admin/",        // per-handler admin auth
	"/api/integrations/", // per-handler bearer-token auth (Jellystreamerr)
	"/api/moderation/",   // per-handler moderator-token auth
	"/admin/",            // admin web app, RequireAdminAuth wrapped
	"/robots.txt",
}

// SetupComplete reports whether first-run setup has stored credentials.
// Until it has, nothing but the setup flow is served.
func SetupComplete() bool {
	configRepository := configrepository.Get()
	return configRepository.GetViewerPasswordHash() != ""
}

// RequireViewerAccess is the router-wide gate. A valid viewer or admin
// session passes; so does valid admin basic auth (the admin web app's
// subresource requests and API tooling). Everything else is turned away —
// browsers toward the login page, API clients with a 401.
func RequireViewerAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if !SetupComplete() {
			// Only the setup flow exists until credentials are set. This
			// closes the fresh-container window: an unconfigured instance
			// exposes nothing.
			if path == "/setup" || strings.HasPrefix(path, "/api/auth/") {
				next.ServeHTTP(w, r)
				return
			}
			deflect(w, r, "/setup")
			return
		}

		for _, prefix := range gateExemptPrefixes {
			if strings.HasPrefix(path, prefix) {
				next.ServeHTTP(w, r)
				return
			}
		}

		if auth.RequestRole(r) != "" {
			next.ServeHTTP(w, r)
			return
		}

		if hasValidAdminBasicAuth(r) {
			next.ServeHTTP(w, r)
			return
		}

		deflect(w, r, "/login")
	})
}

// deflect sends browsers to a page and everything else a 401.
func deflect(w http.ResponseWriter, r *http.Request, page string) {
	if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, page, http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error": "not authenticated"}`))
}

func hasValidAdminBasicAuth(r *http.Request) bool {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}
	configRepository := configrepository.Get()
	return subtle.ConstantTimeCompare([]byte(user), []byte("admin")) == 1 &&
		auth.VerifyPassword(pass, configRepository.GetAdminPassword())
}
