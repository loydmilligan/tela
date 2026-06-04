package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zcag/tela/backend/internal/db"
)

func TestIsPublicPath(t *testing.T) {
	cases := map[string]bool{
		"/api/health":           true,
		"/api/version":          true, // M16.A.1.5 build-metadata probe (public)
		"/api/auth/login":       true,
		"/api/auth/logout":      true,
		"/api/auth/me":          true,
		"/api/auth/anything":    true,
		"/api/share/abc123":     true, // M15.0 token-scoped public read API
		"/share/abc123":         true, // M15.5 bot-UA-gated OG envelope
		"/share/abc123/p/42":    true,
		"/api/diagrams/123/abcdef.png": true, // M13.2 PNG sidecar (public, content-addressed)
		"/api/images/123/abcdef.png":   true, // image sidecar (public, content-addressed)
		"/api/spaces":           false,
		"/api/pages/1":          false,
		"/api/pages/1/backlink": false,
		"/api/pages/1/diagrams": false, // M13.2 PUT lives on the session-gated /api/pages/* path
		"/api/pages/1/images":   false, // POST lives on the session-gated /api/pages/* path
		"/api/search":           false,
		"/api/auth":             false, // no trailing slash — not under /api/auth/
		"/share":                false, // no trailing slash — not under /share/
	}
	for path, want := range cases {
		if got := IsPublicPath(path); got != want {
			t.Errorf("IsPublicPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestCookieSecure_RespectsPublicBaseURLScheme(t *testing.T) {
	cases := []struct {
		env  string
		want bool
	}{
		{"", false},
		{"http://localhost:8780", false},
		{"http://tela.cagdas.io", false},
		{"https://tela.cagdas.io", true},
		{"HTTPS://tela.cagdas.io", true},
	}
	for _, tc := range cases {
		t.Setenv("TELA_PUBLIC_BASE_URL", tc.env)
		if got := CookieSecure(); got != tc.want {
			t.Errorf("CookieSecure() with TELA_PUBLIC_BASE_URL=%q = %v, want %v", tc.env, got, tc.want)
		}
	}
}

func TestMiddleware_RejectsMissingCookie(t *testing.T) {
	d := newAuthTestDB(t)
	mw := Middleware(d, nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not be reached without a session")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%q want 401", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("body=%q missing unauthorized envelope", rec.Body.String())
	}
}

func TestMiddleware_RejectsBogusCookie(t *testing.T) {
	d := newAuthTestDB(t)
	h := Middleware(d, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not be reached with a bogus session id")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "not-a-real-session"})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestMiddleware_BypassesPublicPath(t *testing.T) {
	d := newAuthTestDB(t)
	reached := false
	h := Middleware(d, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	h.ServeHTTP(rec, req)
	if !reached {
		t.Fatalf("public path was blocked by middleware")
	}
}

func TestMiddleware_HappyPathSlidesSessionAndAttachesUser(t *testing.T) {
	ctx := context.Background()
	d := newAuthTestDB(t)

	res, err := BootstrapAdmin(ctx, d, "admin", "pw-1234567890", "", rand.Reader)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !res.Created {
		t.Fatal("bootstrap did not create admin")
	}
	var adminID int64
	if err := d.QueryRowContext(ctx, `SELECT id FROM users WHERE username = 'admin'`).Scan(&adminID); err != nil {
		t.Fatalf("read admin id: %v", err)
	}

	sid, err := CreateSession(ctx, d, adminID, "test-agent")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	var beforeExpiry string
	if err := d.QueryRowContext(ctx, `SELECT expires_at FROM sessions WHERE id = ?`, sid).Scan(&beforeExpiry); err != nil {
		t.Fatalf("read expires_at: %v", err)
	}

	// Force the session to look "older" so slide can be observed (1 day ago).
	if _, err := d.ExecContext(ctx,
		`UPDATE sessions SET expires_at = datetime('now', '+1 day'), last_seen_at = datetime('now', '-1 day') WHERE id = ?`, sid); err != nil {
		t.Fatalf("backdate session: %v", err)
	}

	var gotUserID int64
	var gotUsername string
	h := Middleware(d, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := UserFromContext(r.Context())
		if !ok {
			t.Fatal("UserFromContext returned !ok behind middleware")
		}
		gotUserID = u.ID
		gotUsername = u.Username
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sid})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q want 200", rec.Code, rec.Body.String())
	}
	if gotUserID != adminID || gotUsername != "admin" {
		t.Fatalf("ctx user = (%d, %q); want (%d, %q)", gotUserID, gotUsername, adminID, "admin")
	}

	var afterExpiry, afterSeen string
	if err := d.QueryRowContext(ctx, `SELECT expires_at, last_seen_at FROM sessions WHERE id = ?`, sid).Scan(&afterExpiry, &afterSeen); err != nil {
		t.Fatalf("read after-slide: %v", err)
	}
	// expires_at should be ~30d out (well past the 1-day backdated value).
	if afterExpiry <= "2026-05-21" {
		t.Errorf("expires_at not slid forward: %q", afterExpiry)
	}
}

func TestMiddleware_RejectsInactiveUser(t *testing.T) {
	ctx := context.Background()
	d := newAuthTestDB(t)

	if _, err := BootstrapAdmin(ctx, d, "admin", "pw", "", rand.Reader); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var adminID int64
	if err := d.QueryRowContext(ctx, `SELECT id FROM users WHERE username = 'admin'`).Scan(&adminID); err != nil {
		t.Fatalf("read admin id: %v", err)
	}
	sid, err := CreateSession(ctx, d, adminID, "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := d.ExecContext(ctx, `UPDATE users SET is_active = 0 WHERE id = ?`, adminID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	h := Middleware(d, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not run for deactivated user")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sid})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestLoadSessionAndSlide_RejectsExpired(t *testing.T) {
	ctx := context.Background()
	d := newAuthTestDB(t)

	if _, err := BootstrapAdmin(ctx, d, "admin", "pw", "", rand.Reader); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var adminID int64
	if err := d.QueryRowContext(ctx, `SELECT id FROM users WHERE username = 'admin'`).Scan(&adminID); err != nil {
		t.Fatalf("read admin id: %v", err)
	}
	sid, err := CreateSession(ctx, d, adminID, "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := d.ExecContext(ctx, `UPDATE sessions SET expires_at = datetime('now', '-1 hour') WHERE id = ?`, sid); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if _, err := LoadSessionAndSlide(ctx, d, sid); err == nil {
		t.Fatal("expected ErrInvalidSession for expired session")
	}
}

// TestLoadSessionAndSlide_Concurrent fans out a burst of requests sharing the
// same valid session cookie and asserts every one returns the user — never a
// wrapped DB-busy error. Guards against the regression where the validate
// + slide ran in a DEFERRED tx and lost the write-promotion race under WAL.
func TestLoadSessionAndSlide_Concurrent(t *testing.T) {
	ctx := context.Background()
	// On-disk DB so WAL write-promotion semantics actually apply (the
	// in-memory helper opens a single shared connection that serialises
	// every call and hides the bug). Cleanup via t.TempDir.
	dir := t.TempDir()
	d, err := db.Open(dir + "/tela.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := BootstrapAdmin(ctx, d, "admin", "pw-1234567890", "", rand.Reader); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var adminID int64
	if err := d.QueryRowContext(ctx, `SELECT id FROM users WHERE username='admin'`).Scan(&adminID); err != nil {
		t.Fatalf("read admin id: %v", err)
	}
	sid, err := CreateSession(ctx, d, adminID, "concurrent-test")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	const n = 32
	errs := make(chan error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			u, err := LoadSessionAndSlide(ctx, d, sid)
			if err != nil {
				errs <- err
				return
			}
			if u.ID != adminID {
				errs <- errors.New("wrong user id")
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		// Any non-ErrInvalidSession error here is the race regression
		// (DB busy / locked / write-promotion failure). ErrInvalidSession
		// is the only acceptable failure mode, and it shouldn't fire for
		// a fresh active session — but tolerate it explicitly to make
		// the assertion's intent clear.
		if errors.Is(err, ErrInvalidSession) {
			t.Errorf("unexpected ErrInvalidSession for a fresh active session")
			continue
		}
		t.Errorf("LoadSessionAndSlide returned DB error under concurrent load: %v", err)
	}
}

// TestMiddleware_ReturnsInternalOnDBError asserts that a real DB failure
// (here: the *sql.DB has been closed) surfaces as 500 with the internal
// envelope, not as 401 (which used to bounce users to /login on transient
// errors).
func TestMiddleware_ReturnsInternalOnDBError(t *testing.T) {
	ctx := context.Background()
	d := newAuthTestDB(t)

	if _, err := BootstrapAdmin(ctx, d, "admin", "pw-1234567890", "", rand.Reader); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var adminID int64
	if err := d.QueryRowContext(ctx, `SELECT id FROM users WHERE username='admin'`).Scan(&adminID); err != nil {
		t.Fatalf("read admin id: %v", err)
	}
	sid, err := CreateSession(ctx, d, adminID, "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Close the DB so every subsequent query returns sql.ErrConnDone or
	// similar — simulating a transient backend failure.
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	h := Middleware(d, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not run when session lookup errors")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sid})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%q want 500 (was the DB error collapsed to 401?)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"internal"`) {
		t.Fatalf("body=%q missing internal envelope", rec.Body.String())
	}
}

func TestSetSessionCookie_ClearVsSet(t *testing.T) {
	rec := httptest.NewRecorder()
	SetSessionCookie(rec, "abc")
	setCookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "tela_session=abc") {
		t.Errorf("Set-Cookie missing value: %q", setCookie)
	}
	if !strings.Contains(setCookie, "HttpOnly") {
		t.Errorf("Set-Cookie missing HttpOnly: %q", setCookie)
	}
	if !strings.Contains(setCookie, "SameSite=Lax") {
		t.Errorf("Set-Cookie missing SameSite=Lax: %q", setCookie)
	}

	rec = httptest.NewRecorder()
	SetSessionCookie(rec, "")
	clearCookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(clearCookie, "Max-Age=0") {
		t.Errorf("clear cookie missing Max-Age=0: %q", clearCookie)
	}
}

