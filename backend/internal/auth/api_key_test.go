package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zcag/tela/backend/internal/db"
)

// newOnDiskAuthDB is the on-disk DB variant for tests that exercise the
// bearer middleware's async last_used_at goroutine. modernc.org/sqlite's
// `:memory:` is per-connection, so the goroutine would otherwise see a fresh
// empty DB that hasn't run any migrations.
func newOnDiskAuthDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "tela.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d
}

// seedUserDirect inserts a users row and returns the new id. Used by the
// api_key tests — bootstrap_test.go's helpers expect a hashed password and
// don't expose a non-admin path.
func seedUserDirect(t *testing.T, d *sql.DB, username string, isAdmin bool) int64 {
	t.Helper()
	hash, err := HashPassword("dummy-pw-12345678")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	admin := 0
	if isAdmin {
		admin = 1
	}
	res, err := d.ExecContext(context.Background(), `
		INSERT INTO users (username, password_hash, is_instance_admin, is_active)
		VALUES (?, ?, ?, 1)`, username, hash, admin)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

// seedAPIKeyDB inserts an api_keys row and returns the raw token + new id.
// Mirrors what the /api/api_keys POST handler does so the middleware paths
// exercise the same shape the production CRUD writes.
func seedAPIKeyDB(t *testing.T, d *sql.DB, userID int64, scope string, spaceID *int64, expiresAt *string, name string) (string, int64) {
	t.Helper()
	raw, prefix, hmacHex, err := NewAPIKey(LoadAPIKeySecret())
	if err != nil {
		t.Fatalf("NewAPIKey: %v", err)
	}
	var spaceArg any
	if spaceID != nil {
		spaceArg = *spaceID
	}
	var expArg any
	if expiresAt != nil {
		expArg = *expiresAt
	}
	res, err := d.ExecContext(context.Background(), `
		INSERT INTO api_keys (user_id, name, key_prefix, key_hmac, scope, space_id, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, name, prefix, hmacHex, scope, spaceArg, expArg)
	if err != nil {
		t.Fatalf("insert api_key: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return raw, id
}

func TestNewAPIKey_ShapeAndHMACDeterministic(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	ResetAPIKeySecretCache()
	secret := LoadAPIKeySecret()

	raw, prefix, hmacHex, err := NewAPIKey(secret)
	if err != nil {
		t.Fatalf("NewAPIKey: %v", err)
	}
	if !strings.HasPrefix(raw, TokenPrefix) {
		t.Fatalf("raw %q missing %s prefix", raw, TokenPrefix)
	}
	// "tela_pat_" (9 chars) + 43-char body = 52 chars.
	if len(raw) != 9+43 {
		t.Fatalf("raw length=%d, want 52", len(raw))
	}
	if prefix != raw[:8] {
		t.Fatalf("prefix=%q, want first 8 chars of raw (%q)", prefix, raw[:8])
	}
	// HMAC is deterministic — re-hashing the same raw under the same secret
	// must return the same hex string. Without that property the middleware
	// could never match a stored hash against a freshly-presented token.
	if got := HMACAPIKey(secret, raw); got != hmacHex {
		t.Fatalf("HMAC drift: NewAPIKey=%q, recompute=%q", hmacHex, got)
	}
}

func TestLookupAPIKey_RejectsWrongShape(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "secrettestbytes")
	ResetAPIKeySecretCache()
	d := newOnDiskAuthDB(t)

	for _, bad := range []string{
		"",
		"not-a-token",
		"Bearer something",
		"tela_pat_short",
		"tela_pat_" + strings.Repeat("z", 44), // wrong length
	} {
		_, err := LookupAPIKey(context.Background(), d, LoadAPIKeySecret(), bad)
		if !errors.Is(err, ErrInvalidAPIKey) {
			t.Errorf("LookupAPIKey(%q) err=%v, want ErrInvalidAPIKey", bad, err)
		}
	}
}

func TestLookupAPIKey_ResolvesActiveKey(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "secrettestbytes")
	ResetAPIKeySecretCache()
	d := newOnDiskAuthDB(t)
	ctx := context.Background()

	uid := seedUserDirect(t, d, "alice", true)
	raw, _ := seedAPIKeyDB(t, d, uid, ScopeWrite, nil, nil, "test-key")

	k, err := LookupAPIKey(ctx, d, LoadAPIKeySecret(), raw)
	if err != nil {
		t.Fatalf("LookupAPIKey: %v", err)
	}
	if k.UserID != uid {
		t.Fatalf("UserID=%d, want %d", k.UserID, uid)
	}
	if k.Scope != ScopeWrite {
		t.Fatalf("Scope=%q, want %q", k.Scope, ScopeWrite)
	}
	if k.SpaceID != nil {
		t.Fatalf("SpaceID=%v, want nil", *k.SpaceID)
	}
}

func TestLookupAPIKey_RejectsRevoked(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "secrettestbytes")
	ResetAPIKeySecretCache()
	d := newOnDiskAuthDB(t)
	ctx := context.Background()

	uid := seedUserDirect(t, d, "alice", true)
	raw, id := seedAPIKeyDB(t, d, uid, ScopeWrite, nil, nil, "test-key")

	if _, err := d.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = strftime('%Y-%m-%d %H:%M:%S','now') WHERE id = ?`, id); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := LookupAPIKey(ctx, d, LoadAPIKeySecret(), raw); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("revoked key err=%v, want ErrInvalidAPIKey", err)
	}
}

func TestLookupAPIKey_RejectsExpired(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "secrettestbytes")
	ResetAPIKeySecretCache()
	d := newOnDiskAuthDB(t)
	ctx := context.Background()

	uid := seedUserDirect(t, d, "alice", true)
	past := time.Now().UTC().Add(-time.Hour).Format("2006-01-02 15:04:05")
	raw, _ := seedAPIKeyDB(t, d, uid, ScopeWrite, nil, &past, "expired-key")

	if _, err := LookupAPIKey(ctx, d, LoadAPIKeySecret(), raw); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expired key err=%v, want ErrInvalidAPIKey", err)
	}
}

func TestLookupAPIKey_RejectsInactiveUser(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "secrettestbytes")
	ResetAPIKeySecretCache()
	d := newOnDiskAuthDB(t)
	ctx := context.Background()

	uid := seedUserDirect(t, d, "alice", true)
	raw, _ := seedAPIKeyDB(t, d, uid, ScopeWrite, nil, nil, "test-key")

	if _, err := d.ExecContext(ctx, `UPDATE users SET is_active = 0 WHERE id = ?`, uid); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := LookupAPIKey(ctx, d, LoadAPIKeySecret(), raw); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("inactive user key err=%v, want ErrInvalidAPIKey", err)
	}
}

func TestLookupAPIKey_LastUsedAt_OnDisk(t *testing.T) {
	// Per the Known Pitfall, in-memory SQLite serialises everything so the
	// async last_used_at goroutine never races. Use an on-disk DB so the
	// non-blocking goroutine actually exercises the writer path.
	t.Setenv("TELA_API_KEY_SECRET", "secrettestbytes")
	ResetAPIKeySecretCache()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "tela.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	uid := seedUserDirect(t, d, "alice", true)
	raw, id := seedAPIKeyDB(t, d, uid, ScopeWrite, nil, nil, "test-key")

	if _, err := LookupAPIKey(context.Background(), d, LoadAPIKeySecret(), raw); err != nil {
		t.Fatalf("LookupAPIKey: %v", err)
	}
	// The update is async; poll the column briefly so we don't race the
	// goroutine. last_used_at goes from NULL → a datetime string on first
	// successful lookup.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var last sql.NullString
		if err := d.QueryRow(`SELECT last_used_at FROM api_keys WHERE id = ?`, id).Scan(&last); err == nil {
			if last.Valid && last.String != "" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("last_used_at never populated after LookupAPIKey")
}

func TestMiddleware_BearerHappyPathAttachesContext(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "secrettestbytes")
	ResetAPIKeySecretCache()
	d := newOnDiskAuthDB(t)
	uid := seedUserDirect(t, d, "alice", false)
	raw, _ := seedAPIKeyDB(t, d, uid, ScopeWrite, nil, nil, "k1")

	var (
		gotUserID  int64
		gotScope   string
		gotIsKey   bool
	)
	h := Middleware(d, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := UserFromContext(r.Context()); ok {
			gotUserID = u.ID
		}
		if k, ok := APIKeyFromContext(r.Context()); ok {
			gotIsKey = true
			gotScope = k.Scope
		}
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if gotUserID != uid {
		t.Fatalf("ctx user.id=%d, want %d", gotUserID, uid)
	}
	if !gotIsKey || gotScope != ScopeWrite {
		t.Fatalf("ctx api_key isKey=%v scope=%q, want true/%q", gotIsKey, gotScope, ScopeWrite)
	}
}

func TestMiddleware_BearerScopeReadOnlyBlocksMutation(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "secrettestbytes")
	ResetAPIKeySecretCache()
	d := newOnDiskAuthDB(t)
	uid := seedUserDirect(t, d, "alice", false)
	raw, _ := seedAPIKeyDB(t, d, uid, ScopeRead, nil, nil, "ro")

	reached := false
	h := Middleware(d, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/spaces", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	h.ServeHTTP(rec, req)
	if reached {
		t.Fatalf("inner handler ran on read-scope POST")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"api_key_scope"`) {
		t.Fatalf("body=%q missing api_key_scope envelope", rec.Body.String())
	}

	// GET on the same scope should pass.
	reached = false
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	h.ServeHTTP(rec, req)
	if !reached {
		t.Fatalf("read-scope GET was rejected")
	}
}

func TestMiddleware_BearerInvalidDoesNotFallBackToCookie(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "secrettestbytes")
	ResetAPIKeySecretCache()
	d := newOnDiskAuthDB(t)
	uid := seedUserDirect(t, d, "alice", false)
	sid, err := CreateSession(context.Background(), d, uid, "test")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	h := Middleware(d, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not run when bearer is invalid")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
	// Bogus Bearer token — explicit failure beats accidental session escalation.
	req.Header.Set("Authorization", "Bearer tela_pat_"+strings.Repeat("a", 43))
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sid})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestMiddleware_CookieStillWorksWithoutBearer(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "secrettestbytes")
	ResetAPIKeySecretCache()
	d := newOnDiskAuthDB(t)
	uid := seedUserDirect(t, d, "alice", false)
	sid, err := CreateSession(context.Background(), d, uid, "test")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	reached := false
	h := Middleware(d, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		if _, ok := APIKeyFromContext(r.Context()); ok {
			t.Fatal("cookie session leaked into APIKey context")
		}
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sid})
	h.ServeHTTP(rec, req)
	if !reached {
		t.Fatalf("cookie path blocked, status=%d", rec.Code)
	}
}

// Concurrent bearer auth: a burst of parallel Lookups must all succeed.
// Per the Known Pitfall, sqlite tests that hit the writer concurrently need
// an on-disk DB.
func TestLookupAPIKey_Concurrent(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "secrettestbytes")
	ResetAPIKeySecretCache()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "tela.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	uid := seedUserDirect(t, d, "alice", false)
	raw, _ := seedAPIKeyDB(t, d, uid, ScopeWrite, nil, nil, "k")

	var wg sync.WaitGroup
	const n = 16
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := LookupAPIKey(context.Background(), d, LoadAPIKeySecret(), raw); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent LookupAPIKey: %v", err)
	}
}
