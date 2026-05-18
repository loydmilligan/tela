package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/db"
)

func newAuthTestDB(t *testing.T) *sql.DB {
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

func TestBootstrapAdmin_SeedsAdminAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	d := newAuthTestDB(t)

	if _, err := d.ExecContext(ctx, `INSERT INTO spaces(name, slug) VALUES (?,?), (?,?)`,
		"General", "general", "Engineering", "engineering"); err != nil {
		t.Fatalf("seed spaces: %v", err)
	}

	res1, err := BootstrapAdmin(ctx, d, "", "", rand.Reader)
	if err != nil {
		t.Fatalf("first BootstrapAdmin: %v", err)
	}
	if !res1.Created {
		t.Fatalf("first call: Created=false, want true")
	}
	if res1.Username != "admin" {
		t.Fatalf("first call: Username=%q, want %q", res1.Username, "admin")
	}
	if res1.GeneratedPassword == "" {
		t.Fatalf("first call with empty TELA_ADMIN_PASSWORD should generate one")
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
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM space_members WHERE user_id=? AND role='owner'`, adminID).Scan(&sm); err != nil {
		t.Fatalf("count space_members: %v", err)
	}
	if sm != 2 {
		t.Fatalf("admin owns %d spaces, want 2", sm)
	}

	ok, err := VerifyPassword(res1.GeneratedPassword, mustQueryString(t, d, `SELECT password_hash FROM users WHERE id=?`, adminID))
	if err != nil {
		t.Fatalf("verify generated password: %v", err)
	}
	if !ok {
		t.Fatalf("generated password did not verify")
	}

	res2, err := BootstrapAdmin(ctx, d, "", "", rand.Reader)
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

func TestBootstrapAdmin_UsesEnvPasswordAndUsername(t *testing.T) {
	ctx := context.Background()
	d := newAuthTestDB(t)

	const username = "root"
	const password = "S3cr3t!Provided"
	res, err := BootstrapAdmin(ctx, d, username, password, rand.Reader)
	if err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if !res.Created || res.Username != username {
		t.Fatalf("res=%+v; want Created=true Username=%q", res, username)
	}
	if res.GeneratedPassword != "" {
		t.Fatalf("GeneratedPassword=%q; want empty (env-provided)", res.GeneratedPassword)
	}

	hash := mustQueryString(t, d, `SELECT password_hash FROM users WHERE username=?`, username)
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
