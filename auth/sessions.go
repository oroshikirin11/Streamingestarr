package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

// Role is what a session is allowed to do.
type Role string

const (
	// RoleViewer can watch and chat.
	RoleViewer Role = "viewer"
	// RoleAdmin can additionally configure and moderate. Admin implies viewer.
	RoleAdmin Role = "admin"
)

// SessionTTL is how long a session lives. Sessions persist across restarts
// (SQLite) — a service restart must not log a room full of viewers out.
const SessionTTL = 30 * 24 * time.Hour

// SessionCookieName is the cookie the browser carries the token in.
const SessionCookieName = "sgr_session"

var _db *sql.DB

// Setup creates the session table and starts the expiry sweep.
func Setup(db *sql.DB) {
	_db = db
	createTableSQL := `CREATE TABLE IF NOT EXISTS auth_sessions (
		"token_hash" TEXT NOT NULL PRIMARY KEY,
		"role" TEXT NOT NULL,
		"created_at" INTEGER NOT NULL
	);`
	if _, err := db.Exec(createTableSQL); err != nil {
		log.Fatalln("unable to create auth_sessions table:", err)
	}

	go func() {
		for range time.Tick(time.Hour) {
			sweepExpiredSessions()
		}
	}()
}

// tokens are stored hashed so a database leak does not hand out live sessions.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession stores a new session and returns the client token.
func CreateSession(role Role) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	_, err := _db.Exec("INSERT INTO auth_sessions(token_hash, role, created_at) VALUES(?, ?, ?)",
		hashToken(token), string(role), time.Now().Unix())
	if err != nil {
		return "", err
	}
	return token, nil
}

// SessionRole returns the role for a valid session token, or "" if the
// token is unknown or expired.
func SessionRole(token string) Role {
	if token == "" || _db == nil {
		return ""
	}
	var role string
	var createdAt int64
	row := _db.QueryRow("SELECT role, created_at FROM auth_sessions WHERE token_hash = ?", hashToken(token))
	if err := row.Scan(&role, &createdAt); err != nil {
		return ""
	}
	if time.Since(time.Unix(createdAt, 0)) > SessionTTL {
		DestroySession(token)
		return ""
	}
	return Role(role)
}

// DestroySession removes a session.
func DestroySession(token string) {
	_, _ = _db.Exec("DELETE FROM auth_sessions WHERE token_hash = ?", hashToken(token))
}

// DestroyAllSessions logs everyone out — used when the shared viewer
// password changes ("evict the room") unless a keep token is provided.
func DestroyAllSessions(keepToken string) {
	if keepToken == "" {
		_, _ = _db.Exec("DELETE FROM auth_sessions")
		return
	}
	_, _ = _db.Exec("DELETE FROM auth_sessions WHERE token_hash != ?", hashToken(keepToken))
}

func sweepExpiredSessions() {
	cutoff := time.Now().Add(-SessionTTL).Unix()
	_, _ = _db.Exec("DELETE FROM auth_sessions WHERE created_at < ?", cutoff)
}

// TokenFromRequest pulls the session token from the cookie, or an
// Authorization bearer for non-browser clients.
func TokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(SessionCookieName); err == nil {
		return c.Value
	}
	const bearer = "Bearer "
	if h := r.Header.Get("Authorization"); len(h) > len(bearer) && h[:len(bearer)] == bearer {
		return h[len(bearer):]
	}
	return ""
}

// RequestRole resolves the role carried by a request's session, if any.
func RequestRole(r *http.Request) Role {
	return SessionRole(TokenFromRequest(r))
}

// SetSessionCookie writes the session cookie on a response.
func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(SessionTTL / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
	})
}

// ClearSessionCookie removes the session cookie.
func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
	})
}

func requestIsSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// String satisfies fmt.Stringer for logging.
func (r Role) String() string { return string(r) }

var _ fmt.Stringer = RoleViewer
