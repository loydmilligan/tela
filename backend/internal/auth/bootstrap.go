package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"os"
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
func BootstrapAdmin(ctx context.Context, d *sql.DB, usernameEnv, passwordEnv string, randSrc io.Reader) (BootstrapResult, error) {
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

	res, err := tx.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, is_instance_admin, is_active)
		VALUES (?, ?, 1, 1)
	`, username, hash)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("auth: insert bootstrap admin: %w", err)
	}
	userID, err := res.LastInsertId()
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("auth: bootstrap admin last insert id: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO space_members (space_id, user_id, role)
		SELECT id, ?, 'owner' FROM spaces
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
// Reads TELA_ADMIN_USERNAME / TELA_ADMIN_PASSWORD, calls BootstrapAdmin
// with crypto/rand.Reader, and returns the result.
func BootstrapFromEnv(ctx context.Context, d *sql.DB) (BootstrapResult, error) {
	return BootstrapAdmin(ctx, d, os.Getenv("TELA_ADMIN_USERNAME"), os.Getenv("TELA_ADMIN_PASSWORD"), rand.Reader)
}
