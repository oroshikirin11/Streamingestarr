package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"

	"streamingestarr/auth"
	"streamingestarr/persistence/configrepository"
	"streamingestarr/utils"
	"streamingestarr/webserver/router/middleware"
)

// The login and setup pages are served by the Go binary itself, fully
// inline — they must work before (and without) any web app. Styling
// follows the design tokens (docs/design.md §7): dark base, Sunset Coral.
const authPageTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>%s</title>
<style>
  :root {
    --bg: #17171a; --surface: #202024; --surface-2: #2a2a2f;
    --text: #ececeb; --muted: #9a9a95; --border: #34343a;
    --accent: #e8846b; --danger: #e8705f; --radius: 8px;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; background: var(--bg); color: var(--text);
    font-family: system-ui, sans-serif;
    min-height: 100vh; display: flex; align-items: center; justify-content: center;
  }
  .card {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: 12px; padding: 2rem; width: min(22rem, 90vw);
    box-shadow: 0 0 60px color-mix(in srgb, var(--accent) 8%%, transparent);
  }
  h1 { font-size: 1.1rem; margin: 0 0 .25rem; letter-spacing: .04em; }
  h1 span { color: var(--accent); }
  p.sub { color: var(--muted); font-size: .85rem; margin: 0 0 1.5rem; }
  label { display: block; font-size: .8rem; color: var(--muted); margin: 1rem 0 .3rem; }
  input {
    width: 100%%; padding: .6rem .75rem; border-radius: var(--radius);
    border: 1px solid var(--border); background: var(--surface-2);
    color: var(--text); font-size: .95rem;
  }
  input:focus { outline: none; border-color: var(--accent); }
  button {
    width: 100%%; margin-top: 1.5rem; padding: .65rem;
    border: none; border-radius: var(--radius);
    background: var(--accent); color: #141416; font-weight: 600;
    font-size: .95rem; cursor: pointer;
  }
  button:hover { filter: brightness(1.08); }
  .error { color: var(--danger); font-size: .85rem; margin: 1rem 0 0; min-height: 1.2em; }
</style>
</head>
<body>
<div class="card">
  <h1><span>●</span> Streamingestarr</h1>
  %s
</div>
<script>
document.querySelector('form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const form = e.target;
  const body = Object.fromEntries(new FormData(form));
  const errEl = document.querySelector('.error');
  errEl.textContent = '';
  try {
    const res = await fetch(form.action, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    const data = await res.json();
    if (res.ok && data.success) {
      if (form.hasAttribute('data-savename') && body.username && body.username.toLowerCase() !== 'admin') {
        try { localStorage.setItem('sgr_preferred_name', body.username); } catch {}
      }
      window.location.href = '/'; return;
    }
    errEl.textContent = data.message || 'That did not work.';
  } catch { errEl.textContent = 'Could not reach the server.'; }
});
</script>
</body>
</html>`

const loginFormHTML = `
  <p class="sub">This theater is private. Pick your name, bring the room password.</p>
  <form action="/api/auth/login" method="post" data-savename>
    <label for="username">Your name</label>
    <input id="username" name="username" maxlength="30" autocomplete="username" required autofocus>
    <label for="password">Password</label>
    <input id="password" name="password" type="password" autocomplete="current-password" required>
    <p class="error"></p>
    <button type="submit">Enter</button>
  </form>`

const setupFormHTML = `
  <p class="sub">First run — set the keys to the theater. The room password is shared by everyone who watches; the admin password is yours. At the door, everyone picks their own name.</p>
  <form action="/api/auth/setup" method="post">
    <label for="viewerPassword">Room password</label>
    <input id="viewerPassword" name="viewerPassword" type="password" autocomplete="new-password" required autofocus>
    <label for="adminPassword">Admin password</label>
    <input id="adminPassword" name="adminPassword" type="password" autocomplete="new-password" required minlength="8">
    <p class="error"></p>
    <button type="submit">Open the doors</button>
  </form>`

// GetLoginPage serves the viewer login page.
func GetLoginPage(w http.ResponseWriter, r *http.Request) {
	if !middleware.SetupComplete() {
		http.Redirect(w, r, "/setup", http.StatusTemporaryRedirect)
		return
	}
	if auth.RequestRole(r) != "" {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	serveAuthPage(w, "Streamingestarr — Login", loginFormHTML)
}

// GetSetupPage serves the first-run setup page.
func GetSetupPage(w http.ResponseWriter, r *http.Request) {
	if middleware.SetupComplete() {
		http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
		return
	}
	serveAuthPage(w, "Streamingestarr — Setup", setupFormHTML)
}

func serveAuthPage(w http.ResponseWriter, title, form string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	middleware.DisableCache(w)
	fmt.Fprintf(w, authPageTemplate, title, form)
}

type authRequest struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	ViewerUsername string `json:"viewerUsername"`
	ViewerPassword string `json:"viewerPassword"`
	AdminPassword  string `json:"adminPassword"`
}

func authJSON(w http.ResponseWriter, status int, success bool, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": success, "message": message})
}

// PostAuthLogin verifies the shared viewer login or the admin password and
// hands out a session.
func PostAuthLogin(w http.ResponseWriter, r *http.Request) {
	if !middleware.SetupComplete() {
		authJSON(w, http.StatusForbidden, false, "Setup has not run yet.")
		return
	}

	ip := utils.GetIPAddressFromRequest(r)
	if wait := auth.ThrottleCheck(ip); wait > 0 {
		authJSON(w, http.StatusTooManyRequests, false, fmt.Sprintf("Too many attempts. Try again in %ds.", wait))
		return
	}

	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		authJSON(w, http.StatusBadRequest, false, "Invalid request.")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Username) > 64 {
		authJSON(w, http.StatusBadRequest, false, "Pick a name (1-64 characters).")
		return
	}

	// Two fields, one door: the name is yours, the password decides the
	// role. "admin" + the admin password is the admin; the shared room
	// password lets anyone in as a viewer under the name they picked
	// (which becomes their proposed chat name).
	configRepository := configrepository.Get()
	role := auth.Role("")
	switch {
	case strings.EqualFold(req.Username, "admin") && auth.VerifyPassword(req.Password, configRepository.GetAdminPassword()):
		role = auth.RoleAdmin
	case !strings.EqualFold(req.Username, "admin") && auth.VerifyPassword(req.Password, configRepository.GetViewerPasswordHash()):
		role = auth.RoleViewer
	default:
		auth.ThrottleFail(ip)
		authJSON(w, http.StatusUnauthorized, false, "Wrong name or password.")
		return
	}

	auth.ThrottleReset(ip)
	token, err := auth.CreateSession(role)
	if err != nil {
		log.Errorln("unable to create session:", err)
		authJSON(w, http.StatusInternalServerError, false, "Something broke. Try again.")
		return
	}
	auth.SetSessionCookie(w, r, token)
	authJSON(w, http.StatusOK, true, "")
}

// PostAuthSetup performs first-run setup: it stores the shared viewer login
// and the admin password, then logs the caller in as admin. Once run it is
// permanently closed; credential changes go through the admin API.
func PostAuthSetup(w http.ResponseWriter, r *http.Request) {
	if middleware.SetupComplete() {
		authJSON(w, http.StatusForbidden, false, "Setup already completed.")
		return
	}

	ip := utils.GetIPAddressFromRequest(r)
	if wait := auth.ThrottleCheck(ip); wait > 0 {
		authJSON(w, http.StatusTooManyRequests, false, fmt.Sprintf("Too many attempts. Try again in %ds.", wait))
		return
	}

	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		authJSON(w, http.StatusBadRequest, false, "Invalid request.")
		return
	}

	viewerHash, err := auth.HashPassword(req.ViewerPassword)
	if err != nil {
		authJSON(w, http.StatusBadRequest, false, "Room password cannot be empty.")
		return
	}
	if len(req.AdminPassword) < auth.MinPasswordLength {
		authJSON(w, http.StatusBadRequest, false, "Admin password must be at least 8 characters.")
		return
	}

	configRepository := configrepository.Get()
	if err := configRepository.SetAdminPassword(req.AdminPassword); err != nil {
		log.Errorln("unable to store admin password:", err)
		authJSON(w, http.StatusInternalServerError, false, "Unable to store credentials.")
		return
	}
	// The hash write is the commit point: SetupComplete() keys on it, so
	// it goes last and the gate stays closed if anything above failed.
	if err := configRepository.SetViewerPasswordHash(viewerHash); err != nil {
		log.Errorln("unable to store viewer password:", err)
		authJSON(w, http.StatusInternalServerError, false, "Unable to store credentials.")
		return
	}

	token, err := auth.CreateSession(auth.RoleAdmin)
	if err == nil {
		auth.SetSessionCookie(w, r, token)
	}
	log.Infoln("First-run setup completed; the doors are open.")
	authJSON(w, http.StatusOK, true, "")
}

// SetViewerLogin lets the admin change the shared room password.
// Changing it evicts every session except the caller's — that is the
// design's "change the password to clear the room" lever.
func SetViewerLogin(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		authJSON(w, http.StatusBadRequest, false, "Invalid request.")
		return
	}

	hash, err := auth.HashPassword(req.ViewerPassword)
	if err != nil {
		authJSON(w, http.StatusBadRequest, false, "Room password cannot be empty.")
		return
	}

	configRepository := configrepository.Get()
	if err := configRepository.SetViewerPasswordHash(hash); err != nil {
		authJSON(w, http.StatusInternalServerError, false, "Unable to store credentials.")
		return
	}

	auth.DestroyAllSessions(auth.TokenFromRequest(r))
	log.Infoln("Room password changed; all other sessions ended.")
	authJSON(w, http.StatusOK, true, "")
}

// SetChatNameReservationDays lets the admin adjust how long an unseen chat
// name stays reserved. Body: {"value": <days>}; 0 disables expiry.
func SetChatNameReservationDays(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Value *int `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Value == nil {
		authJSON(w, http.StatusBadRequest, false, "Invalid request; send {\"value\": days}.")
		return
	}
	days := *req.Value
	if days < 0 || days > 3650 {
		authJSON(w, http.StatusBadRequest, false, "Days must be between 0 (never expire) and 3650.")
		return
	}
	stored := days
	if days == 0 {
		stored = -1 // 0 in the datastore means "unset"; -1 encodes "never expire".
	}
	if err := configrepository.Get().SetChatNameReservationDays(stored); err != nil {
		authJSON(w, http.StatusInternalServerError, false, "Unable to store setting.")
		return
	}
	authJSON(w, http.StatusOK, true, "")
}

// SetVideoSegmentFormat lets the admin switch the HLS segment container.
// Body: {"value": "ts"|"fmp4"}. fMP4 is required for AV1/HEVC delivery.
// Takes effect on the next stream (and next offline transition).
func SetVideoSegmentFormat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Value != "ts" && req.Value != "fmp4") {
		authJSON(w, http.StatusBadRequest, false, "Send {\"value\": \"ts\"} or {\"value\": \"fmp4\"}.")
		return
	}
	if err := configrepository.Get().SetVideoSegmentFormat(req.Value); err != nil {
		authJSON(w, http.StatusInternalServerError, false, "Unable to store setting.")
		return
	}
	authJSON(w, http.StatusOK, true, "")
}

// SetSRTEnabled toggles the SRT ingest listener (takes effect on restart).
func SetSRTEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Value *bool `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Value == nil {
		authJSON(w, http.StatusBadRequest, false, "Send {\"value\": true|false}.")
		return
	}
	if err := configrepository.Get().SetSRTServerEnabled(*req.Value); err != nil {
		authJSON(w, http.StatusInternalServerError, false, "Unable to store setting.")
		return
	}
	authJSON(w, http.StatusOK, true, "Takes effect after a restart.")
}

// SetSRTPort sets the SRT ingest UDP port (takes effect on restart).
func SetSRTPort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Value *int `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Value == nil || *req.Value < 1 || *req.Value > 65535 {
		authJSON(w, http.StatusBadRequest, false, "Send {\"value\": port}.")
		return
	}
	if err := configrepository.Get().SetSRTServerPort(*req.Value); err != nil {
		authJSON(w, http.StatusInternalServerError, false, "Unable to store setting.")
		return
	}
	authJSON(w, http.StatusOK, true, "Takes effect after a restart.")
}

// PostAuthLogout destroys the caller's session.
func PostAuthLogout(w http.ResponseWriter, r *http.Request) {
	if token := auth.TokenFromRequest(r); token != "" {
		auth.DestroySession(token)
	}
	auth.ClearSessionCookie(w, r)
	authJSON(w, http.StatusOK, true, "")
}

// GetAuthStatus reports gate state for clients.
func GetAuthStatus(w http.ResponseWriter, r *http.Request) {
	role := auth.RequestRole(r)
	w.Header().Set("Content-Type", "application/json")
	middleware.DisableCache(w)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"setupComplete": middleware.SetupComplete(),
		"authenticated": role != "",
		"role":          role.String(),
	})
}
