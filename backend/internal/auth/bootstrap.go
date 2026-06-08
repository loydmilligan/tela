package auth

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// BootstrapResult describes what BootstrapAdmin did, for the caller's
// banner-printing convenience.
type BootstrapResult struct {
	Created  bool   // true if an admin row was inserted on this call
	Username string // the admin username (always set when Created)
}

// BootstrapAdmin seeds the instance's first admin from env config, but ONLY
// when an admin password is explicitly provided (passwordEnv != ""). It is
// idempotent: subsequent calls are no-ops once any user exists.
//
// When passwordEnv is empty the function is a deliberate no-op: it leaves the
// users table untouched so the first-run web setup wizard (POST /api/setup) can
// create the first admin instead. This replaces the old behaviour of generating
// a random password and logging it. An operator who still wants the env path
// just sets TELA_ADMIN_PASSWORD; existing instances already have users and so
// short-circuit on the count check regardless.
//
// When triggered, all work happens in a single tx:
//  1. INSERT the admin user (is_instance_admin=1, is_active=1).
//  2. Backfill space_members(role='owner') for every existing space.
//
// Wrapping both steps avoids the half-state where the admin exists but
// doesn't own existing spaces — a crash between the user insert and the
// backfill would leave the system unusable.
//
// Inputs:
//   - usernameEnv: TELA_ADMIN_USERNAME value (empty → default "admin").
//   - passwordEnv: TELA_ADMIN_PASSWORD value (empty → skip, let the wizard run).
func BootstrapAdmin(ctx context.Context, d *sql.DB, usernameEnv, passwordEnv, emailEnv string) (BootstrapResult, error) {
	// No admin password configured → leave the table empty for the web setup
	// wizard. Never auto-create a credential-less or random-password admin.
	if passwordEnv == "" {
		return BootstrapResult{Created: false}, nil
	}

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("auth: begin bootstrap tx: %w", err)
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return BootstrapResult{}, fmt.Errorf("auth: count users: %w", err)
	}
	if count > 0 {
		return BootstrapResult{Created: false}, nil
	}

	username := usernameEnv
	if username == "" {
		username = "admin"
	}

	hash, err := HashPassword(passwordEnv)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("auth: hash bootstrap password: %w", err)
	}

	// A bootstrap email is treated as pre-confirmed (the operator set it), so
	// the admin can sign in immediately. Empty → NULL email, a username-only
	// admin that is exempt from the login email gate.
	var email, verifiedAt any
	if e := strings.ToLower(strings.TrimSpace(emailEnv)); e != "" {
		email = e
		verifiedAt = nowStamp()
	}

	var userID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO users (username, email, email_verified_at, password_hash, is_instance_admin, is_active)
		VALUES ($1, $2, $3, $4, 1, 1)
		RETURNING id
	`, username, email, verifiedAt, hash).Scan(&userID); err != nil {
		return BootstrapResult{}, fmt.Errorf("auth: insert bootstrap admin: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO space_members (space_id, user_id, role)
		SELECT id, $1, 'owner' FROM spaces
	`, userID); err != nil {
		return BootstrapResult{}, fmt.Errorf("auth: backfill space_members: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return BootstrapResult{}, fmt.Errorf("auth: commit bootstrap tx: %w", err)
	}

	return BootstrapResult{Created: true, Username: username}, nil
}

// BootstrapFromEnv is the convenience wrapper used from main.go.
// Reads TELA_ADMIN_USERNAME / TELA_ADMIN_PASSWORD / TELA_ADMIN_EMAIL and calls
// BootstrapAdmin. With TELA_ADMIN_PASSWORD unset it is a no-op (the web setup
// wizard creates the first admin instead).
func BootstrapFromEnv(ctx context.Context, d *sql.DB) (BootstrapResult, error) {
	return BootstrapAdmin(ctx, d,
		os.Getenv("TELA_ADMIN_USERNAME"), os.Getenv("TELA_ADMIN_PASSWORD"), os.Getenv("TELA_ADMIN_EMAIL"))
}

// BackfillAdminEmailFromEnv assigns TELA_ADMIN_EMAIL to the bootstrap admin on
// an instance that was created before email auth existed. No-op when the env is
// unset, or when the admin already has an email, or when the address is already
// taken (the unique index rejects it — logged, not fatal). Idempotent.
func BackfillAdminEmailFromEnv(ctx context.Context, d *sql.DB) {
	email := strings.ToLower(strings.TrimSpace(os.Getenv("TELA_ADMIN_EMAIL")))
	if email == "" {
		return
	}
	username := os.Getenv("TELA_ADMIN_USERNAME")
	if username == "" {
		username = "admin"
	}
	res, err := d.ExecContext(ctx, `
		UPDATE users
		   SET email = $1, email_verified_at = COALESCE(email_verified_at, $2), updated_at = $3
		 WHERE username = $4 AND is_instance_admin = 1 AND email IS NULL`,
		email, nowStamp(), nowStamp(), username)
	if err != nil {
		slog.Error("auth: backfill admin email", "err", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("auth: assigned TELA_ADMIN_EMAIL to admin", "username", username)
	}
}

// nowStamp is the UTC timestamp format the rest of the schema uses
// (datetime('now') equivalent), so values inserted from Go sort/compare
// against SQLite-generated ones.
func nowStamp() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}
