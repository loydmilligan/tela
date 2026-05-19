package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// CookieName is the canonical session cookie. HttpOnly + SameSite=Lax + Path=/,
// Secure flag conditional on TELA_PUBLIC_BASE_URL being https://.
const CookieName = "tela_session"

// SessionMaxAge is the rolling lifetime extended on every authenticated
// request and used as the cookie's MaxAge.
const SessionMaxAge = 30 * 24 * time.Hour

const sessionIDBytes = 32

// User is the subset of the users row that authenticated handlers need.
type User struct {
	ID              int64
	Username        string
	IsInstanceAdmin bool
}

type contextKey int

const userCtxKey contextKey = 1

// WithUser returns a context that carries u. Used by Middleware after a
// session has been validated.
func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

// UserFromContext pulls the authenticated user out of ctx. The second return
// is false when called from a handler that did not run behind Middleware.
func UserFromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(userCtxKey).(*User)
	return u, ok
}

// ErrInvalidSession signals that the session id was missing, expired, or
// pointed at an inactive/deleted user. Callers should treat this as 401.
var ErrInvalidSession = errors.New("auth: invalid session")

// LoadSessionAndSlide validates sessionID, slides expires_at forward by
// SessionMaxAge, and stamps last_seen_at — all in a single tx so concurrent
// requests can't see a half-extended session. Returns the resolved user.
func LoadSessionAndSlide(ctx context.Context, d *sql.DB, sessionID string) (*User, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var (
		userID  int64
		isAdmin int
	)
	err = tx.QueryRowContext(ctx, `
		SELECT u.id, u.is_instance_admin
		  FROM sessions s
		  JOIN users u ON u.id = s.user_id
		 WHERE s.id = ?
		   AND s.expires_at > datetime('now')
		   AND u.is_active = 1`, sessionID).Scan(&userID, &isAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidSession
	}
	if err != nil {
		return nil, err
	}

	newExpires := time.Now().UTC().Add(SessionMaxAge).Format("2006-01-02 15:04:05")
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET expires_at = ?, last_seen_at = datetime('now') WHERE id = ?`,
		newExpires, sessionID); err != nil {
		return nil, err
	}

	var username string
	if err := tx.QueryRowContext(ctx, `SELECT username FROM users WHERE id = ?`, userID).Scan(&username); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &User{ID: userID, Username: username, IsInstanceAdmin: isAdmin == 1}, nil
}

// CreateSession inserts a fresh session row for userID. The id is base64url
// of 32 random bytes — collision-resistant and URL-safe for the cookie value.
func CreateSession(ctx context.Context, d *sql.DB, userID int64, userAgent string) (string, error) {
	buf := make([]byte, sessionIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(buf)
	expires := time.Now().UTC().Add(SessionMaxAge).Format("2006-01-02 15:04:05")
	if _, err := d.ExecContext(ctx, `
		INSERT INTO sessions(id, user_id, expires_at, last_seen_at, user_agent)
		VALUES (?, ?, ?, datetime('now'), ?)`,
		id, userID, expires, userAgent); err != nil {
		return "", err
	}
	return id, nil
}

// DeleteSession removes a session row by id. No error if the row is missing.
func DeleteSession(ctx context.Context, d *sql.DB, sessionID string) error {
	_, err := d.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	return err
}

// CookieSecure reports whether the session cookie should carry the Secure
// flag. True only when TELA_PUBLIC_BASE_URL has an https:// scheme — local
// dev (http://localhost:8780) keeps the cookie usable.
func CookieSecure() bool {
	return strings.HasPrefix(strings.ToLower(os.Getenv("TELA_PUBLIC_BASE_URL")), "https://")
}

// SetSessionCookie writes the canonical session cookie. Pass id="" to clear.
func SetSessionCookie(w http.ResponseWriter, id string) {
	c := &http.Cookie{
		Name:     CookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   CookieSecure(),
	}
	if id == "" {
		c.MaxAge = -1
	} else {
		c.MaxAge = int(SessionMaxAge.Seconds())
	}
	http.SetCookie(w, c)
}

// IsPublicPath returns true for routes that bypass Middleware. Per the M6.1
// brief: /api/health and everything under /api/auth/. Login is anonymous
// by definition; logout + me handle cookie presence themselves.
func IsPublicPath(p string) bool {
	if p == "/api/health" {
		return true
	}
	return strings.HasPrefix(p, "/api/auth/")
}

// Middleware enforces the session cookie on every request except IsPublicPath.
// Validates and slides the session, attaches the User to context, and writes
// the canonical 401 envelope on any failure.
func Middleware(d *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsPublicPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			c, err := r.Cookie(CookieName)
			if err != nil || c.Value == "" {
				writeUnauthorized(w)
				return
			}
			u, err := LoadSessionAndSlide(r.Context(), d, c.Value)
			if err != nil {
				writeUnauthorized(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
		})
	}
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized","code":"unauthorized"}`))
}

var (
	dummyHashOnce sync.Once
	dummyHashStr  string
)

// DummyVerifyHash returns a precomputed argon2id hash used to keep login
// response time constant when the queried username does not exist. Computed
// lazily once per process so test runs aren't penalised on import.
func DummyVerifyHash() string {
	dummyHashOnce.Do(func() {
		h, err := HashPassword("tela-bogus-dummy-not-a-real-password")
		if err != nil {
			// HashPassword only fails when crypto/rand fails — fall back to
			// a static well-formed encoding so the timing path still runs.
			h = argonPrefix + "AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		}
		dummyHashStr = h
	})
	return dummyHashStr
}
