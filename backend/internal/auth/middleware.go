package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"log"
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

// LoadSessionAndSlide validates sessionID and best-effort slides expires_at
// forward by SessionMaxAge + stamps last_seen_at. Returns ErrInvalidSession
// for missing/expired/inactive; any other error is a real DB problem the
// caller should propagate as 500.
//
// Implementation note: the validate + slide used to share a DEFERRED tx, but
// under SQLite WAL two parallel requests sharing one cookie both pass the
// SELECT in read mode, then both try to upgrade to writer on the UPDATE —
// the loser hits SQLITE_BUSY because busy_timeout doesn't apply to write
// promotion of a DEFERRED tx. Splitting into a single-statement SELECT plus
// a best-effort UPDATE serializes naturally via per-statement locking. The
// slide is an optimisation (rolling-window extension), not a correctness
// invariant — a failed slide just means the session expires earlier than it
// would have; the current request still completes.
func LoadSessionAndSlide(ctx context.Context, d *sql.DB, sessionID string) (*User, error) {
	var (
		userID   int64
		username string
		isAdmin  int
	)
	err := d.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.is_instance_admin
		  FROM sessions s
		  JOIN users u ON u.id = s.user_id
		 WHERE s.id = ?
		   AND s.expires_at > datetime('now')
		   AND u.is_active = 1`, sessionID).Scan(&userID, &username, &isAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidSession
	}
	if err != nil {
		return nil, err
	}

	newExpires := time.Now().UTC().Add(SessionMaxAge).Format("2006-01-02 15:04:05")
	if _, err := d.ExecContext(ctx,
		`UPDATE sessions SET expires_at = ?, last_seen_at = datetime('now') WHERE id = ?`,
		newExpires, sessionID); err != nil {
		log.Printf("auth: session slide failed for %s: %v", sessionID, err)
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

// sessionExec is the subset of *sql.DB / *sql.Tx the session helpers use, so
// the same call works whether the caller already has an open tx or not.
type sessionExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// DeleteUserSessions removes every session for userID. Used by admin
// password reset + admin deactivate so the affected user is logged out of
// every device immediately.
func DeleteUserSessions(ctx context.Context, d sessionExec, userID int64) error {
	_, err := d.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

// DeleteUserSessionsExcept removes every session for userID except exceptID.
// Used by self-service password change + "logout everywhere" so the caller's
// own session survives.
func DeleteUserSessionsExcept(ctx context.Context, d sessionExec, userID int64, exceptID string) error {
	_, err := d.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND id != ?`, userID, exceptID)
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
			if errors.Is(err, ErrInvalidSession) {
				writeUnauthorized(w)
				return
			}
			if err != nil {
				log.Printf("auth: session lookup failed: %v", err)
				writeInternal(w)
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

func writeInternal(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(`{"error":"internal error","code":"internal"}`))
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
