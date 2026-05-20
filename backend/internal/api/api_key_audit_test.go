package api

import (
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
	"time"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/db"
)

// newWiredServerOnDiskWithSrv mirrors newWiredServerOnDisk but also exposes
// the *Server so tests can reach the AuditWriter and Flush() between
// bearer-authed requests and "did the row land?" assertions.
func newWiredServerOnDiskWithSrv(t *testing.T) (*httptest.Server, *sql.DB, *Server) {
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
	h, srv := HandlerWithServer(d)
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	t.Cleanup(srv.auditWriter.Close)
	return ts, d, srv
}

// auditEntry is the wire shape for one row of the GET .../audit response.
type auditEntry struct {
	ID         int64  `json:"id"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	StatusCode int    `json:"status_code"`
	Ts         string `json:"ts"`
}

type auditEnvelope struct {
	Audit []auditEntry `json:"audit"`
}

// seedAPIKeyForUser inserts a bearer key for uid and returns (rawKey, id).
// Mirrors the api_keys_test.go pattern but consolidated here so the audit
// tests can stand alone.
func seedAPIKeyForUser(t *testing.T, d *sql.DB, uid int64, scope string, spaceID *int64) (string, int64) {
	t.Helper()
	raw, prefix, hmacHex, err := auth.NewAPIKey(auth.LoadAPIKeySecret())
	if err != nil {
		t.Fatalf("gen api key: %v", err)
	}
	var spaceArg any
	if spaceID != nil {
		spaceArg = *spaceID
	}
	res, err := d.ExecContext(context.Background(), `
		INSERT INTO api_keys (user_id, name, key_prefix, key_hmac, scope, space_id)
		VALUES (?, ?, ?, ?, ?, ?)`,
		uid, "test-key", prefix, hmacHex, scope, spaceArg)
	if err != nil {
		t.Fatalf("insert api_key: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return raw, id
}

// TestAPIKeyAudit_BearerRequestLogsOneRow exercises the canonical flow: a
// successful bearer-authed request creates exactly one audit row.
func TestAPIKeyAudit_BearerRequestLogsOneRow(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d, srv := newWiredServerOnDiskWithSrv(t)
	uid := seedUser(t, d, "alice", "alicepw12", false)
	rawKey, keyID := seedAPIKeyForUser(t, d, uid, auth.ScopeRead, nil)

	resp := bearerRequest(t, http.MethodGet, ts.URL+"/api/spaces", rawKey, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status=%d want 200", resp.StatusCode)
	}

	srv.AuditWriter().Flush()

	var n int
	if err := d.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM api_key_audit WHERE api_key_id = ?`, keyID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("audit row count=%d, want 1", n)
	}
	var (
		method     string
		path       string
		statusCode int
	)
	if err := d.QueryRowContext(context.Background(),
		`SELECT method, path, status_code FROM api_key_audit WHERE api_key_id = ?`, keyID).
		Scan(&method, &path, &statusCode); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if method != "GET" || path != "/api/spaces" || statusCode != 200 {
		t.Fatalf("row mismatch: method=%q path=%q status=%d", method, path, statusCode)
	}
}

// TestAPIKeyAudit_LogsAllStatusCodes locks the rule from the brief: 4xx + 5xx
// statuses are recorded too, not just 200.
func TestAPIKeyAudit_LogsAllStatusCodes(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d, srv := newWiredServerOnDiskWithSrv(t)
	uid := seedUser(t, d, "alice", "alicepw12", false)
	// read-scope key — POST will get 403 api_key_scope; an unknown page id
	// will get 404 (well, 403 from the membership gate first, but still a 4xx
	// — what matters is that something other than 200 lands in the audit).
	rawKey, keyID := seedAPIKeyForUser(t, d, uid, auth.ScopeRead, nil)

	// GET works → 200.
	resp := bearerRequest(t, http.MethodGet, ts.URL+"/api/spaces", rawKey, "")
	resp.Body.Close()

	// POST blocked by scope → 403 (api_key_scope, written by middleware
	// itself, before any handler runs).
	resp = bearerRequest(t, http.MethodPost, ts.URL+"/api/spaces", rawKey, `{"name":"X","slug":"x"}`)
	resp.Body.Close()

	// GET a missing page → 4xx from the handler (membership check 403s
	// because alice isn't a member of any space owning page 99999).
	resp = bearerRequest(t, http.MethodGet, ts.URL+"/api/pages/99999", rawKey, "")
	resp.Body.Close()

	srv.AuditWriter().Flush()

	rows, err := d.QueryContext(context.Background(),
		`SELECT method, path, status_code FROM api_key_audit WHERE api_key_id = ? ORDER BY id ASC`, keyID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var (
			method     string
			path       string
			statusCode int
		)
		if err := rows.Scan(&method, &path, &statusCode); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, fmt.Sprintf("%s %s %d", method, path, statusCode))
	}
	if len(got) != 3 {
		t.Fatalf("got %d audit rows, want 3: %v", len(got), got)
	}
	// First should be 200, second should be 403, third 4xx.
	if !strings.HasSuffix(got[0], " 200") {
		t.Errorf("row 0 = %q, want trailing 200", got[0])
	}
	if !strings.HasSuffix(got[1], " 403") {
		t.Errorf("row 1 = %q, want trailing 403 (scope refusal)", got[1])
	}
	// The third call is GET /api/pages/99999 — membership-gate 403 (alice has
	// no space memberships, so any per-page access 403s before hitting NOT
	// FOUND). Anything 4xx is fine for our purposes.
	if !strings.Contains(got[2], "/api/pages/99999") {
		t.Errorf("row 2 = %q, want /api/pages/99999 path", got[2])
	}
	parts := strings.Split(got[2], " ")
	if !strings.HasPrefix(parts[len(parts)-1], "4") {
		t.Errorf("row 2 final status = %q, want 4xx", parts[len(parts)-1])
	}
}

// TestAPIKeyAudit_GetAuditByOwner — owner of the key can read its audit log.
func TestAPIKeyAudit_GetAuditByOwner(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d, srv := newWiredServerOnDiskWithSrv(t)
	uid := seedUser(t, d, "alice", "alicepw12", false)
	rawKey, keyID := seedAPIKeyForUser(t, d, uid, auth.ScopeRead, nil)

	// Generate one audit row via a bearer-authed request.
	resp := bearerRequest(t, http.MethodGet, ts.URL+"/api/spaces", rawKey, "")
	resp.Body.Close()
	srv.AuditWriter().Flush()

	// Owner cookie-session reads the audit log.
	aliceC := loginClient(t, ts, "alice", "alicepw12")
	r, err := aliceC.Get(fmt.Sprintf("%s/api/api_keys/%d/audit", ts.URL, keyID))
	if err != nil {
		t.Fatalf("get audit: %v", err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(r.Body)
		t.Fatalf("status=%d body=%s want 200", r.StatusCode, body)
	}
	var env auditEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Audit) != 1 {
		t.Fatalf("audit len=%d, want 1", len(env.Audit))
	}
	got := env.Audit[0]
	if got.Method != "GET" || got.Path != "/api/spaces" || got.StatusCode != 200 {
		t.Fatalf("audit[0] = %+v, want GET /api/spaces 200", got)
	}
}

// TestAPIKeyAudit_GetAuditByAdmin — an instance-admin can read any user's key
// audit log.
func TestAPIKeyAudit_GetAuditByAdmin(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d, _ := newWiredServerOnDiskWithSrv(t)
	seedUser(t, d, "admin", "adminpw12", true)
	bobID := seedUser(t, d, "bob", "bobpw1234", false)
	_, keyID := seedAPIKeyForUser(t, d, bobID, auth.ScopeRead, nil)

	adminC := loginClient(t, ts, "admin", "adminpw12")
	r, err := adminC.Get(fmt.Sprintf("%s/api/api_keys/%d/audit", ts.URL, keyID))
	if err != nil {
		t.Fatalf("get audit: %v", err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(r.Body)
		t.Fatalf("status=%d body=%s want 200", r.StatusCode, body)
	}
	var env auditEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Audit == nil {
		t.Fatalf("audit field missing from response")
	}
}

// TestAPIKeyAudit_GetAuditByStranger — a non-owner non-admin gets 404 (404
// over 403 keeps the key's existence hidden from a probe).
func TestAPIKeyAudit_GetAuditByStranger(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d, _ := newWiredServerOnDiskWithSrv(t)
	aliceID := seedUser(t, d, "alice", "alicepw12", false)
	seedUser(t, d, "bob", "bobpw1234", false)
	_, keyID := seedAPIKeyForUser(t, d, aliceID, auth.ScopeRead, nil)

	bobC := loginClient(t, ts, "bob", "bobpw1234")
	r, err := bobC.Get(fmt.Sprintf("%s/api/api_keys/%d/audit", ts.URL, keyID))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d want 404", r.StatusCode)
	}
}

// TestAPIKeyAudit_PaginatesByBefore — `?before=` excludes rows at or after the
// given ts and returns them most-recent-first.
func TestAPIKeyAudit_PaginatesByBefore(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d, _ := newWiredServerOnDiskWithSrv(t)
	uid := seedUser(t, d, "alice", "alicepw12", false)
	_, keyID := seedAPIKeyForUser(t, d, uid, auth.ScopeRead, nil)
	aliceC := loginClient(t, ts, "alice", "alicepw12")

	// Seed three audit rows directly with explicit ts values, oldest first.
	ctx := context.Background()
	rows := []struct {
		path string
		ts   string
	}{
		{"/a", "2026-05-19 10:00:00"},
		{"/b", "2026-05-19 10:00:01"},
		{"/c", "2026-05-19 10:00:02"},
	}
	for _, row := range rows {
		if _, err := d.ExecContext(ctx, `
			INSERT INTO api_key_audit (api_key_id, method, path, status_code, ts)
			VALUES (?, 'GET', ?, 200, ?)`, keyID, row.path, row.ts); err != nil {
			t.Fatalf("insert %s: %v", row.path, err)
		}
	}

	// No pagination → all three, most-recent first.
	r, _ := aliceC.Get(fmt.Sprintf("%s/api/api_keys/%d/audit", ts.URL, keyID))
	var env auditEnvelope
	_ = json.NewDecoder(r.Body).Decode(&env)
	r.Body.Close()
	if len(env.Audit) != 3 {
		t.Fatalf("no-pagination len=%d, want 3", len(env.Audit))
	}
	if env.Audit[0].Path != "/c" || env.Audit[2].Path != "/a" {
		t.Fatalf("ordering wrong: paths = %s, %s, %s — want /c, /b, /a",
			env.Audit[0].Path, env.Audit[1].Path, env.Audit[2].Path)
	}

	// before=middle ts → only /a.
	r, _ = aliceC.Get(fmt.Sprintf("%s/api/api_keys/%d/audit?before=%s",
		ts.URL, keyID, "2026-05-19+10%3A00%3A01"))
	_ = json.NewDecoder(r.Body).Decode(&env)
	r.Body.Close()
	if len(env.Audit) != 1 || env.Audit[0].Path != "/a" {
		t.Fatalf("before=10:00:01 paths = %+v, want exactly /a", env.Audit)
	}

	// limit=1 → only the most recent (/c).
	r, _ = aliceC.Get(fmt.Sprintf("%s/api/api_keys/%d/audit?limit=1", ts.URL, keyID))
	_ = json.NewDecoder(r.Body).Decode(&env)
	r.Body.Close()
	if len(env.Audit) != 1 || env.Audit[0].Path != "/c" {
		t.Fatalf("limit=1 paths = %+v, want exactly /c", env.Audit)
	}
}

// TestAPIKeyAudit_RejectsInvalidQuery — bad `limit` / bad `before`.
func TestAPIKeyAudit_RejectsInvalidQuery(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d, _ := newWiredServerOnDiskWithSrv(t)
	uid := seedUser(t, d, "alice", "alicepw12", false)
	_, keyID := seedAPIKeyForUser(t, d, uid, auth.ScopeRead, nil)
	aliceC := loginClient(t, ts, "alice", "alicepw12")

	cases := []struct {
		name string
		url  string
	}{
		{"non-integer limit", fmt.Sprintf("%s/api/api_keys/%d/audit?limit=abc", ts.URL, keyID)},
		{"zero limit", fmt.Sprintf("%s/api/api_keys/%d/audit?limit=0", ts.URL, keyID)},
		{"negative limit", fmt.Sprintf("%s/api/api_keys/%d/audit?limit=-5", ts.URL, keyID)},
		{"bad before", fmt.Sprintf("%s/api/api_keys/%d/audit?before=tomorrow", ts.URL, keyID)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := aliceC.Get(tc.url)
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			if r.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s want 400", r.StatusCode, body)
			}
			if !strings.Contains(string(body), `"code":"bad_request"`) {
				t.Fatalf("body=%s missing bad_request code", body)
			}
		})
	}
}

// TestAPIKeyAudit_BearerReadScopeBlocked — a read-scope bearer key cannot
// read its own audit (or anyone else's). Locks the doctrine that audit is a
// human-session-only surface.
func TestAPIKeyAudit_BearerReadScopeBlocked(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d, _ := newWiredServerOnDiskWithSrv(t)
	uid := seedUser(t, d, "alice", "alicepw12", true)
	rawKey, keyID := seedAPIKeyForUser(t, d, uid, auth.ScopeRead, nil)

	r := bearerRequest(t, http.MethodGet,
		fmt.Sprintf("%s/api/api_keys/%d/audit", ts.URL, keyID), rawKey, "")
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d body=%s want 403", r.StatusCode, body)
	}
	if !strings.Contains(string(body), `"code":"api_key_scope"`) {
		t.Fatalf("body=%s missing api_key_scope code", body)
	}
}

// TestAPIKeyAudit_MissingKey404 — GET against a non-existent key id returns
// the same 404 a stranger would get against a real key.
func TestAPIKeyAudit_MissingKey404(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d, _ := newWiredServerOnDiskWithSrv(t)
	seedUser(t, d, "admin", "adminpw12", true)
	adminC := loginClient(t, ts, "admin", "adminpw12")

	r, err := adminC.Get(ts.URL + "/api/api_keys/99999/audit")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d want 404", r.StatusCode)
	}
}

// TestAPIKeyAudit_TsRoundTripFormat — the ts string we read back matches the
// canonical YYYY-MM-DD HH:MM:SS wire format, so client paginators can pass it
// straight back as `?before=`.
func TestAPIKeyAudit_TsRoundTripFormat(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d, srv := newWiredServerOnDiskWithSrv(t)
	uid := seedUser(t, d, "alice", "alicepw12", false)
	rawKey, keyID := seedAPIKeyForUser(t, d, uid, auth.ScopeRead, nil)

	resp := bearerRequest(t, http.MethodGet, ts.URL+"/api/spaces", rawKey, "")
	resp.Body.Close()
	srv.AuditWriter().Flush()

	aliceC := loginClient(t, ts, "alice", "alicepw12")
	r, _ := aliceC.Get(fmt.Sprintf("%s/api/api_keys/%d/audit", ts.URL, keyID))
	var env auditEnvelope
	_ = json.NewDecoder(r.Body).Decode(&env)
	r.Body.Close()
	if len(env.Audit) != 1 {
		t.Fatalf("audit len=%d, want 1", len(env.Audit))
	}
	if _, err := time.Parse("2006-01-02 15:04:05", env.Audit[0].Ts); err != nil {
		t.Fatalf("ts=%q does not parse as YYYY-MM-DD HH:MM:SS: %v", env.Audit[0].Ts, err)
	}
}
