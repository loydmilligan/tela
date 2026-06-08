package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// BootstrapResult describes what BootstrapAdmin did, for the caller's
// banner-printing convenience.
type BootstrapResult struct {
	Created           bool   // true if an admin row was inserted on this call
	Username          string // the admin username (always set when Created)
	GeneratedPassword string // non-empty only when a random password was generated
}

// BootstrapAdmin ensures the instance has exactly one bootstrap admin user
// when the users table is empty. It is idempotent: subsequent calls are
// no-ops once any user exists.
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
//   - passwordEnv: TELA_ADMIN_PASSWORD value (empty → generate a 24-char
//     URL-safe random password via crypto/rand).
//
// randSrc is used for the generated-password path; pass crypto/rand.Reader
// in production. Tests can inject deterministic readers.
func BootstrapAdmin(ctx context.Context, d *sql.DB, usernameEnv, passwordEnv, emailEnv string, randSrc io.Reader) (BootstrapResult, error) {
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

	password := passwordEnv
	generated := false
	if password == "" {
		pw, err := generatePassword(randSrc, 18)
		if err != nil {
			return BootstrapResult{}, err
		}
		password = pw
		generated = true
	}

	hash, err := HashPassword(password)
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

	out := BootstrapResult{Created: true, Username: username}
	if generated {
		out.GeneratedPassword = password
	}
	return out, nil
}

// generatePassword returns a URL-safe random string. byteLen=18 yields
// a 24-character base64 (RawURLEncoding) output.
func generatePassword(r io.Reader, byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", fmt.Errorf("auth: read random password bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// BootstrapFromEnv is the convenience wrapper used from main.go.
// Reads TELA_ADMIN_USERNAME / TELA_ADMIN_PASSWORD / TELA_ADMIN_EMAIL, calls
// BootstrapAdmin with crypto/rand.Reader, and returns the result.
func BootstrapFromEnv(ctx context.Context, d *sql.DB) (BootstrapResult, error) {
	return BootstrapAdmin(ctx, d,
		os.Getenv("TELA_ADMIN_USERNAME"), os.Getenv("TELA_ADMIN_PASSWORD"), os.Getenv("TELA_ADMIN_EMAIL"),
		rand.Reader)
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
