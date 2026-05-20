package auth

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/zcag/tela/backend/internal/db"
)

// auditTestDB returns an on-disk DB with migrations applied + a single
// api_key row pre-seeded. On-disk so the audit goroutine's
// context.Background() ExecContext sees the same data the test writes —
// modernc.org/sqlite's `:memory:` is per-connection (see Known Pitfall).
func auditTestDB(t *testing.T) (*sql.DB, int64) {
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
	ctx := context.Background()
	if _, err := d.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, is_instance_admin, is_active)
		VALUES ('alice', 'x', 1, 1)`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var uid int64
	if err := d.QueryRowContext(ctx, `SELECT id FROM users WHERE username='alice'`).Scan(&uid); err != nil {
		t.Fatalf("read user id: %v", err)
	}
	res, err := d.ExecContext(ctx, `
		INSERT INTO api_keys (user_id, name, key_prefix, key_hmac, scope)
		VALUES (?, 'k', 'tela_pat', 'deadbeefdeadbeef', 'write')`, uid)
	if err != nil {
		t.Fatalf("seed api_key: %v", err)
	}
	keyID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return d, keyID
}

func TestAuditWriter_SubmitInsertsRow(t *testing.T) {
	d, keyID := auditTestDB(t)
	aw := NewAuditWriter(d)
	t.Cleanup(aw.Close)

	aw.Submit(AuditEvent{APIKeyID: keyID, Method: "GET", Path: "/api/spaces", StatusCode: 200})
	aw.Flush()

	var (
		method     string
		path       string
		statusCode int
		ts         string
	)
	if err := d.QueryRowContext(context.Background(),
		`SELECT method, path, status_code, ts FROM api_key_audit WHERE api_key_id = ?`, keyID).
		Scan(&method, &path, &statusCode, &ts); err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	if method != "GET" || path != "/api/spaces" || statusCode != 200 {
		t.Fatalf("row mismatch: method=%q path=%q status=%d", method, path, statusCode)
	}
	if len(ts) != len("2006-01-02 15:04:05") {
		t.Fatalf("ts=%q does not match YYYY-MM-DD HH:MM:SS shape", ts)
	}
}

func TestAuditWriter_SubmitAfterClose_IsNoop(t *testing.T) {
	d, keyID := auditTestDB(t)
	aw := NewAuditWriter(d)
	aw.Close()
	// Should not panic / block on a closed channel.
	aw.Submit(AuditEvent{APIKeyID: keyID, Method: "GET", Path: "/x", StatusCode: 200})
	var n int
	if err := d.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM api_key_audit WHERE api_key_id = ?`, keyID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("post-close Submit landed %d rows, want 0", n)
	}
}

func TestAuditWriter_NilReceiver_IsSafe(t *testing.T) {
	var aw *AuditWriter
	// All three methods must be no-op safe on a nil receiver — Middleware
	// can be called with aw=nil from tests that don't care about audit.
	aw.Submit(AuditEvent{APIKeyID: 1, Method: "GET", Path: "/x", StatusCode: 200})
	if got := aw.Flush(); got != 0 {
		t.Fatalf("Flush on nil = %d, want 0", got)
	}
	aw.Close()
}

func TestAuditWriter_DropsWhenBufferFull(t *testing.T) {
	// Construct the writer without launching the worker — the channel fills
	// deterministically so we can assert drop semantics without racing the
	// drain goroutine. Production code never sees this state because
	// NewAuditWriter always starts the worker.
	aw := &AuditWriter{ch: make(chan auditMsg, auditBufferSize)}
	for i := 0; i < auditBufferSize; i++ {
		aw.Submit(AuditEvent{APIKeyID: 1, Method: "GET", Path: "/", StatusCode: 200})
	}
	// Buffer is now full. Next Submit must hit the default branch and bump
	// the drop counter.
	aw.Submit(AuditEvent{APIKeyID: 1, Method: "GET", Path: "/", StatusCode: 200})
	if got := aw.dropped.Load(); got != 1 {
		t.Fatalf("dropped=%d after one overflow, want 1", got)
	}
	// Subsequent overflows accumulate.
	for i := 0; i < 10; i++ {
		aw.Submit(AuditEvent{APIKeyID: 1, Method: "GET", Path: "/", StatusCode: 200})
	}
	if got := aw.dropped.Load(); got != 11 {
		t.Fatalf("dropped=%d after eleven overflows, want 11", got)
	}
}

func TestPurgeAuditOlderThan_DropsExpired(t *testing.T) {
	d, keyID := auditTestDB(t)
	ctx := context.Background()
	// Old row (35 days ago) and a fresh row.
	if _, err := d.ExecContext(ctx, `
		INSERT INTO api_key_audit (api_key_id, method, path, status_code, ts)
		VALUES (?, 'GET', '/old', 200, datetime('now', '-35 days'))`, keyID); err != nil {
		t.Fatalf("insert old: %v", err)
	}
	if _, err := d.ExecContext(ctx, `
		INSERT INTO api_key_audit (api_key_id, method, path, status_code, ts)
		VALUES (?, 'GET', '/new', 200, datetime('now'))`, keyID); err != nil {
		t.Fatalf("insert new: %v", err)
	}

	if err := purgeAuditOlderThan(ctx, d, 30); err != nil {
		t.Fatalf("purge: %v", err)
	}

	var n int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_key_audit WHERE api_key_id = ?`, keyID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("post-purge count=%d, want 1 (only the fresh row should survive)", n)
	}
	var path string
	if err := d.QueryRowContext(ctx, `SELECT path FROM api_key_audit WHERE api_key_id = ?`, keyID).Scan(&path); err != nil {
		t.Fatalf("read surviving path: %v", err)
	}
	if path != "/new" {
		t.Fatalf("surviving row path=%q, want /new", path)
	}
}

func TestPurgeAuditOlderThan_ZeroDaysIsNoop(t *testing.T) {
	d, keyID := auditTestDB(t)
	if _, err := d.ExecContext(context.Background(), `
		INSERT INTO api_key_audit (api_key_id, method, path, status_code, ts)
		VALUES (?, 'GET', '/x', 200, datetime('now', '-1 day'))`, keyID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := purgeAuditOlderThan(context.Background(), d, 0); err != nil {
		t.Fatalf("purge: %v", err)
	}
	var n int
	if err := d.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM api_key_audit`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("zero-day purge dropped a row (n=%d, want 1)", n)
	}
}

func TestStartAuditGC_RespectsEnv(t *testing.T) {
	// Sanity check: an invalid env value falls back to the default, a valid
	// value parses, and a non-positive value falls back. We can't easily
	// observe the days field directly without exposing it, so this is just
	// a smoke test that StartAuditGC doesn't crash on weird inputs. The
	// actual sweep behaviour is covered by purgeAuditOlderThan.
	d, _ := auditTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, v := range []string{"", "abc", "0", "-5", "7"} {
		t.Setenv("TELA_API_KEY_AUDIT_DAYS", v)
		StartAuditGC(ctx, d)
	}
	// Give the goroutines a beat to do their initial sweep, then cancel.
	time.Sleep(50 * time.Millisecond)
}
