package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/db"
)

// newWiredServerOnDisk is the on-disk variant of newWiredServer. Required for
// bearer-auth tests because LookupAPIKey kicks off an async goroutine to
// stamp last_used_at — modernc.org/sqlite's `:memory:` is per-connection, so
// the async goroutine sees a fresh empty DB without the api_keys table. The
// on-disk file is shared across every connection in the pool.
func newWiredServerOnDisk(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	t.Setenv("TELA_SHARE_SECRET", "tela-test-share-secret-fixed-32-byte!")
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "tela.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ts := httptest.NewServer(Handler(d))
	t.Cleanup(ts.Close)
	return ts, d
}

// apiKeyEnvelope is the wire shape returned by CRUD endpoints.
type apiKeyEnvelope struct {
	APIKey apiKeyDTO `json:"api_key"`
}

type apiKeyListEnvelope struct {
	APIKeys []apiKeyDTO `json:"api_keys"`
}

// bearerRequest issues a request against the wired httptest.Server using a
// raw bearer token instead of a session cookie. Mirrors the
// Authorization-header path the production middleware takes.
func bearerRequest(t *testing.T, method, url, token, body string) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// TestAPIKeys_CreateThenListReturnsKeyOnce locks the cardinal rule: the raw
// `key` field is populated on create, then NEVER again. Subsequent list
// responses return key_prefix only.
func TestAPIKeys_CreateThenListReturnsKeyOnce(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d := newWiredServer(t)
	seedUser(t, d, "admin", "adminpw12", true)
	adminC := loginClient(t, ts, "admin", "adminpw12")

	resp, err := adminC.Post(ts.URL+"/api/api_keys", "application/json",
		strings.NewReader(`{"name":"agent-1","scope":"write"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create status=%d body=%s", resp.StatusCode, b)
	}
	var created apiKeyEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.APIKey.Key == "" {
		t.Fatalf("create response missing raw key")
	}
	if !strings.HasPrefix(created.APIKey.Key, "tela_pat_") {
		t.Fatalf("raw key %q missing tela_pat_ prefix", created.APIKey.Key)
	}
	if created.APIKey.KeyPrefix != created.APIKey.Key[:8] {
		t.Fatalf("key_prefix=%q does not match raw[:8]=%q", created.APIKey.KeyPrefix, created.APIKey.Key[:8])
	}

	// LIST must not return the raw key.
	resp, err = adminC.Get(ts.URL + "/api/api_keys")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", resp.StatusCode)
	}
	var listed apiKeyListEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.APIKeys) != 1 {
		t.Fatalf("list len=%d, want 1", len(listed.APIKeys))
	}
	if listed.APIKeys[0].Key != "" {
		t.Fatalf("list response leaked raw key: %q", listed.APIKeys[0].Key)
	}
	if listed.APIKeys[0].KeyPrefix != created.APIKey.KeyPrefix {
		t.Fatalf("list key_prefix=%q, want %q", listed.APIKeys[0].KeyPrefix, created.APIKey.KeyPrefix)
	}
}

// TestAPIKeys_RevokeIsSoftAndIdempotent — DELETE stamps revoked_at; a second
// DELETE is a no-op 204; bearer auth on the revoked key returns 401.
func TestAPIKeys_RevokeIsSoftAndIdempotent(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	// Bearer auth at end of test → on-disk required for the async last_used_at
	// goroutine (see newWiredServerOnDisk for the modernc.org/sqlite caveat).
	ts, d := newWiredServerOnDisk(t)
	seedUser(t, d, "admin", "adminpw12", true)
	adminC := loginClient(t, ts, "admin", "adminpw12")

	resp, err := adminC.Post(ts.URL+"/api/api_keys", "application/json",
		strings.NewReader(`{"name":"agent-1","scope":"read"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var created apiKeyEnvelope
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	rawKey := created.APIKey.Key
	keyID := created.APIKey.ID

	// First DELETE → 204, stamp revoked_at.
	resp, err = deleteReq(adminC, fmt.Sprintf("%s/api/api_keys/%d", ts.URL, keyID))
	if err != nil {
		t.Fatalf("delete 1: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete 1 status=%d want 204", resp.StatusCode)
	}

	// Second DELETE → 204 again (idempotent).
	resp, err = deleteReq(adminC, fmt.Sprintf("%s/api/api_keys/%d", ts.URL, keyID))
	if err != nil {
		t.Fatalf("delete 2: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete 2 status=%d want 204", resp.StatusCode)
	}

	// Bearer auth with the revoked key → 401.
	r := bearerRequest(t, http.MethodGet, ts.URL+"/api/spaces", rawKey, "")
	defer r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked bearer status=%d want 401", r.StatusCode)
	}
}

// TestAPIKeys_BearerReadScopeBlocksWrite asserts the canonical scope rule
// in the wired stack: GET /api/spaces succeeds, POST /api/spaces 403s with
// api_key_scope. Mirrors the middleware-level test but goes through the
// real handler chain.
func TestAPIKeys_BearerReadScopeBlocksWrite(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	// On-disk DB: bearer middleware's async last_used_at goroutine spawns a
	// second connection — modernc.org/sqlite's `:memory:` is per-connection
	// so the goroutine wouldn't see the api_keys table.
	ts, d := newWiredServerOnDisk(t)
	uid := seedUser(t, d, "admin", "adminpw12", true)
	rawKey, _, _, _ := auth.NewAPIKey(auth.LoadAPIKeySecret())
	if _, err := d.ExecContext(context.Background(), `
		INSERT INTO api_keys (user_id, name, key_prefix, key_hmac, scope, space_id)
		VALUES (?, 'k', ?, ?, 'read', NULL)`,
		uid, rawKey[:8], auth.HMACAPIKey(auth.LoadAPIKeySecret(), rawKey)); err != nil {
		t.Fatalf("seed key: %v", err)
	}

	// GET passes (read scope + GET).
	r := bearerRequest(t, http.MethodGet, ts.URL+"/api/spaces", rawKey, "")
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("GET status=%d want 200", r.StatusCode)
	}
	// POST blocks with api_key_scope.
	r = bearerRequest(t, http.MethodPost, ts.URL+"/api/spaces", rawKey, `{"name":"X","slug":"x"}`)
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("POST status=%d want 403 (body=%s)", r.StatusCode, body)
	}
	if !strings.Contains(string(body), `"code":"api_key_scope"`) {
		t.Fatalf("POST body=%s missing api_key_scope envelope", body)
	}
}

// TestAPIKeys_BearerSpaceRestrictionGatesPageAccess asserts the per-space
// gate: a key restricted to space A returns 403 api_key_space_scope when
// asked about any page in space B, even if the underlying user is a member
// of both.
func TestAPIKeys_BearerSpaceRestrictionGatesPageAccess(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d := newWiredServerOnDisk(t)
	uid := seedUser(t, d, "admin", "adminpw12", true)
	spaceA := seedSpace(t, d, "A", "a", uid)
	spaceB := seedSpace(t, d, "B", "b", uid)
	pageInA := seedPageInSpace(t, d, spaceA, nil, "p-a", "body-a")
	pageInB := seedPageInSpace(t, d, spaceB, nil, "p-b", "body-b")

	// Issue a key scoped to spaceA only.
	rawKey, _, _, _ := auth.NewAPIKey(auth.LoadAPIKeySecret())
	if _, err := d.ExecContext(context.Background(), `
		INSERT INTO api_keys (user_id, name, key_prefix, key_hmac, scope, space_id)
		VALUES (?, 'spaceA-only', ?, ?, 'write', ?)`,
		uid, rawKey[:8], auth.HMACAPIKey(auth.LoadAPIKeySecret(), rawKey), spaceA); err != nil {
		t.Fatalf("seed key: %v", err)
	}

	// Page in scoped space → 200.
	r := bearerRequest(t, http.MethodGet, fmt.Sprintf("%s/api/pages/%d", ts.URL, pageInA), rawKey, "")
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("in-scope page status=%d want 200", r.StatusCode)
	}
	// Page outside scoped space → 403 api_key_space_scope.
	r = bearerRequest(t, http.MethodGet, fmt.Sprintf("%s/api/pages/%d", ts.URL, pageInB), rawKey, "")
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("out-of-scope page status=%d want 403 (body=%s)", r.StatusCode, body)
	}
	if !strings.Contains(string(body), `"code":"api_key_space_scope"`) {
		t.Fatalf("out-of-scope body=%s missing api_key_space_scope envelope", body)
	}
	// Cross-space list query (?space_id=B) → 403 api_key_space_scope too.
	r = bearerRequest(t, http.MethodGet, fmt.Sprintf("%s/api/pages?space_id=%d", ts.URL, spaceB), rawKey, "")
	body, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("list?space_id=B status=%d want 403 (body=%s)", r.StatusCode, body)
	}
	if !strings.Contains(string(body), `"code":"api_key_space_scope"`) {
		t.Fatalf("list body=%s missing api_key_space_scope envelope", body)
	}
}

// TestAPIKeys_NonAdminCookieRejected ensures a logged-in non-admin can't
// create / list keys.
func TestAPIKeys_NonAdminCookieRejected(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d := newWiredServer(t)
	seedUser(t, d, "bob", "bobpw1234", false)
	c := loginClient(t, ts, "bob", "bobpw1234")

	r, _ := c.Post(ts.URL+"/api/api_keys", "application/json",
		strings.NewReader(`{"name":"x","scope":"read"}`))
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin POST status=%d want 403", r.StatusCode)
	}
	r.Body.Close()
	r, _ = c.Get(ts.URL + "/api/api_keys")
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin GET status=%d want 403", r.StatusCode)
	}
	r.Body.Close()
}

// TestAPIKeys_CreateValidatesPayload — happy-path validation: bad scope,
// empty name, non-future expires_at, non-existent space_id all 400.
func TestAPIKeys_CreateValidatesPayload(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d := newWiredServer(t)
	seedUser(t, d, "admin", "adminpw12", true)
	adminC := loginClient(t, ts, "admin", "adminpw12")

	cases := []struct {
		name string
		body string
		want int
		code string
	}{
		{"empty name", `{"name":"","scope":"read"}`, http.StatusBadRequest, "bad_request"},
		{"bad scope", `{"name":"k","scope":"superuser"}`, http.StatusBadRequest, "bad_request"},
		{"past expires_at", `{"name":"k","scope":"read","expires_at":"2000-01-01 00:00:00"}`, http.StatusBadRequest, "bad_request"},
		{"unknown space", `{"name":"k","scope":"read","space_id":9999}`, http.StatusBadRequest, "space_not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := adminC.Post(ts.URL+"/api/api_keys", "application/json", bytes.NewBufferString(tc.body))
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			if r.StatusCode != tc.want {
				t.Fatalf("status=%d want %d (body=%s)", r.StatusCode, tc.want, body)
			}
			if !strings.Contains(string(body), `"code":"`+tc.code+`"`) {
				t.Fatalf("body=%s missing %s code", body, tc.code)
			}
		})
	}
}
