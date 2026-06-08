package auth

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/testdb"
)

func newAuthTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return testdb.New(t)
}

// With TELA_ADMIN_PASSWORD provided, the env bootstrap seeds the admin and
// owns every existing space; a second call is a no-op (idempotent).
func TestBootstrapAdmin_SeedsAdminFromEnvPasswordAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	d := newAuthTestDB(t)

	if _, err := d.ExecContext(ctx, `INSERT INTO spaces(name, slug) VALUES ($1,$2), ($3,$4)`,
		"General", "general", "Engineering", "engineering"); err != nil {
		t.Fatalf("seed spaces: %v", err)
	}

	const password = "S3cr3t!Provided"
	res1, err := BootstrapAdmin(ctx, d, "", password, "")
	if err != nil {
		t.Fatalf("first BootstrapAdmin: %v", err)
	}
	if !res1.Created {
		t.Fatalf("first call: Created=false, want true")
	}
	if res1.Username != "admin" {
		t.Fatalf("first call: Username=%q, want %q", res1.Username, "admin")
	}

	var userCount, adminID, isAdmin, isActive int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("users count=%d, want 1", userCount)
	}
	if err := d.QueryRowContext(ctx, `SELECT id, is_instance_admin, is_active FROM users WHERE username='admin'`).Scan(&adminID, &isAdmin, &isActive); err != nil {
		t.Fatalf("query admin row: %v", err)
	}
	if isAdmin != 1 || isActive != 1 {
		t.Fatalf("admin row: is_instance_admin=%d is_active=%d; want 1/1", isAdmin, isActive)
	}

	var sm int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM space_members WHERE user_id=$1 AND role='owner'`, adminID).Scan(&sm); err != nil {
		t.Fatalf("count space_members: %v", err)
	}
	if sm != 2 {
		t.Fatalf("admin owns %d spaces, want 2", sm)
	}

	ok, err := VerifyPassword(password, mustQueryString(t, d, `SELECT password_hash FROM users WHERE id=$1`, adminID))
	if err != nil {
		t.Fatalf("verify env password: %v", err)
	}
	if !ok {
		t.Fatalf("env password did not verify")
	}

	res2, err := BootstrapAdmin(ctx, d, "", password, "")
	if err != nil {
		t.Fatalf("second BootstrapAdmin: %v", err)
	}
	if res2.Created {
		t.Fatalf("second call: Created=true, want false (idempotent)")
	}

	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		t.Fatalf("recount users: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("users count after second call=%d, want 1", userCount)
	}
}

// With TELA_ADMIN_PASSWORD unset, the bootstrap is a no-op: the users table is
// left empty so the web setup wizard can create the first admin.
func TestBootstrapAdmin_SkipsWhenNoPassword(t *testing.T) {
	ctx := context.Background()
	d := newAuthTestDB(t)

	res, err := BootstrapAdmin(ctx, d, "", "", "")
	if err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if res.Created {
		t.Fatalf("Created=true; want false (no password env → wizard handles setup)")
	}
	var n int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 0 {
		t.Fatalf("users count=%d, want 0 (no admin auto-created)", n)
	}
}

func TestBootstrapAdmin_UsesEnvPasswordAndUsername(t *testing.T) {
	ctx := context.Background()
	d := newAuthTestDB(t)

	const username = "root"
	const password = "S3cr3t!Provided"
	res, err := BootstrapAdmin(ctx, d, username, password, "")
	if err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if !res.Created || res.Username != username {
		t.Fatalf("res=%+v; want Created=true Username=%q", res, username)
	}

	hash := mustQueryString(t, d, `SELECT password_hash FROM users WHERE username=$1`, username)
	if !strings.HasPrefix(hash, argonPrefix) {
		t.Fatalf("stored hash missing argon prefix: %q", hash)
	}
	ok, err := VerifyPassword(password, hash)
	if err != nil || !ok {
		t.Fatalf("VerifyPassword(env pw) ok=%v err=%v", ok, err)
	}
}

func mustQueryString(t *testing.T, d *sql.DB, q string, args ...any) string {
	t.Helper()
	var s string
	if err := d.QueryRowContext(context.Background(), q, args...).Scan(&s); err != nil {
		t.Fatalf("query %s: %v", q, err)
	}
	return s
}
