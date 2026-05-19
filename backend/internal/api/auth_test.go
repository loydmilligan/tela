package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
)

// TestMe_ReturnsUnauthorizedOnInvalidSession sanity-checks the existing 401
// path: cookie present but no matching session row → 401 + unauthorized
// envelope.
func TestMe_ReturnsUnauthorizedOnInvalidSession(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "not-a-real-session-id"})
	rec := recordHandler(srv.Me, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%q want 401", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("body=%q missing unauthorized envelope", rec.Body.String())
	}
}

// TestMe_ReturnsInternalOnDBError asserts that a real DB failure (here: the
// *sql.DB has been closed) surfaces as 500 with the internal envelope, not
// 401. Mirrors TestMiddleware_ReturnsInternalOnDBError in the auth package;
// auth.Me bypasses Middleware so it needs its own coverage of the same
// 401-vs-500 split.
func TestMe_ReturnsInternalOnDBError(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)

	// Close the DB so every subsequent query returns sql.ErrConnDone or
	// similar — simulating a transient backend failure. The cookie value
	// is never validated against a real session row because the DB-closed
	// error fires first.
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "any-cookie-value"})
	rec := recordHandler(srv.Me, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%q want 500 (was the DB error collapsed to 401?)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"internal"`) {
		t.Fatalf("body=%q missing internal envelope", rec.Body.String())
	}
}
