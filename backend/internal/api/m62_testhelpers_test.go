package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/db"
)

// newAPITestDB opens an in-memory SQLite and runs every migration. Shared by
// the M6.2 admin/me/space-member tests.
func newAPITestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d
}

// seedUser inserts a user row directly and returns the new id. The password
// is hashed via auth.HashPassword so VerifyPassword paths can be exercised.
func seedUser(t *testing.T, d *sql.DB, username, password string, isAdmin bool) int64 {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash %s: %v", username, err)
	}
	admin := 0
	if isAdmin {
		admin = 1
	}
	res, err := d.ExecContext(context.Background(),
		`INSERT INTO users (username, password_hash, is_instance_admin, is_active)
		 VALUES (?, ?, ?, 1)`, username, hash, admin)
	if err != nil {
		t.Fatalf("insert user %s: %v", username, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

// seedSpace inserts a space and (optionally) makes ownerID its owner.
func seedSpace(t *testing.T, d *sql.DB, name, slug string, ownerID int64) int64 {
	t.Helper()
	res, err := d.ExecContext(context.Background(),
		`INSERT INTO spaces (name, slug) VALUES (?, ?)`, name, slug)
	if err != nil {
		t.Fatalf("insert space: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	if ownerID > 0 {
		seedMember(t, d, id, ownerID, "owner")
	}
	return id
}

func seedMember(t *testing.T, d *sql.DB, spaceID, userID int64, role string) {
	t.Helper()
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO space_members (space_id, user_id, role) VALUES (?, ?, ?)`,
		spaceID, userID, role); err != nil {
		t.Fatalf("insert space_members: %v", err)
	}
}

// seedSession inserts a session row for userID via the production helper and
// returns its id.
func seedSession(t *testing.T, d *sql.DB, userID int64) string {
	t.Helper()
	id, err := auth.CreateSession(context.Background(), d, userID, "test-agent")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return id
}

// userRequest builds an *http.Request whose context already carries u — the
// API handlers expect the middleware to have done this, so tests can skip
// the cookie-cookies dance and call handlers directly.
func userRequest(method, path, body string, u *auth.User) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req = req.WithContext(auth.WithUser(req.Context(), u))
	return req
}

// userRequestWithSession is userRequest but additionally attaches the M6.1
// session cookie so handlers that read currentSessionID find it.
func userRequestWithSession(method, path, body string, u *auth.User, sessionID string) *http.Request {
	req := userRequest(method, path, body, u)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessionID})
	return req
}

func recordHandler(h http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// routedRecorder runs req through a mux registered with pattern + handler so
// PathValue("id") / PathValue("user_id") resolves correctly inside the
// handler. Use this for tests that exercise routes with {id} / {user_id}.
func routedRecorder(pattern string, h http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc(pattern, h)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// authUser builds a *auth.User from an id + username pair. Tests pass this
// into userRequest / userRequestWithSession to simulate a middleware-wrapped
// request.
func authUser(id int64, username string, isAdmin bool) *auth.User {
	return &auth.User{ID: id, Username: username, IsInstanceAdmin: isAdmin}
}
