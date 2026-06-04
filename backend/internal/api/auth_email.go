package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/mail"
	"os"
	"strings"
	"time"
)

// Email-verification and password-reset tokens. The raw token is a 32-byte
// base64url string carried only in the emailed link; the DB stores just its
// SHA-256 hash, so reading the email_tokens table never yields a usable token.
const (
	verifyTokenTTL = 24 * time.Hour
	resetTokenTTL  = 1 * time.Hour
)

// emailTokenExec is the subset of *sql.DB / *sql.Tx the token helpers need, so
// the same call works inside or outside a transaction.
type emailTokenExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// newEmailToken returns (raw, hash). raw goes in the email link; hash is what
// gets persisted.
func newEmailToken() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashEmailToken(raw), nil
}

// nowStamp is the SQLite datetime('now') equivalent for values written from
// Go, so they sort/compare against schema-generated timestamps.
func nowStamp() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}

func hashEmailToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// createEmailToken inserts a fresh token of kind ("verify"|"reset") for userID
// and returns the raw token to embed in the link. ttl bounds its lifetime.
func createEmailToken(ctx context.Context, ex emailTokenExec, userID int64, kind string, ttl time.Duration) (string, error) {
	raw, hash, err := newEmailToken()
	if err != nil {
		return "", err
	}
	expires := time.Now().UTC().Add(ttl).Format("2006-01-02 15:04:05")
	if _, err := ex.ExecContext(ctx,
		`INSERT INTO email_tokens(user_id, kind, token_hash, expires_at) VALUES (?, ?, ?, ?)`,
		userID, kind, hash, expires); err != nil {
		return "", err
	}
	return raw, nil
}

// errTokenInvalid is returned by consumeEmailToken for a missing, wrong-kind,
// expired, or already-consumed token — all indistinguishable to the caller by
// design (a 400 "invalid or expired link").
var errTokenInvalid = errors.New("auth: invalid or expired token")

// consumeEmailToken validates raw against kind and marks it consumed in the
// same statement-pair, returning the owning user id. Must run inside a tx so
// the select-then-mark can't be raced into a double-use. Returns
// errTokenInvalid for any non-usable token.
func consumeEmailToken(ctx context.Context, tx *sql.Tx, kind, raw string) (int64, error) {
	hash := hashEmailToken(raw)
	var (
		id     int64
		userID int64
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id, user_id FROM email_tokens
		 WHERE token_hash = ? AND kind = ?
		   AND consumed_at IS NULL
		   AND expires_at > datetime('now')`, hash, kind).Scan(&id, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errTokenInvalid
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE email_tokens SET consumed_at = datetime('now') WHERE id = ?`, id); err != nil {
		return 0, err
	}
	return userID, nil
}

// normalizeEmail lowercases and trims an address for storage + comparison, so
// the case-insensitive unique index and login lookup agree.
func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// validEmail does a light RFC 5322 address parse — enough to reject obvious
// garbage without pretending to verify deliverability (that's what the
// confirmation email is for).
func validEmail(s string) bool {
	if len(s) > 254 {
		return false
	}
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}

// appBaseURL is the public origin the SPA is served from, used to build verify
// / reset links. Mirrors shareURLFor's resolution: TELA_PUBLIC_BASE_URL, with a
// localhost fallback so dev still produces a complete (if log-only) link.
func appBaseURL() string {
	base := strings.TrimRight(os.Getenv("TELA_PUBLIC_BASE_URL"), "/")
	if base == "" {
		base = "http://localhost:8780"
	}
	return base
}

func verifyLink(rawToken string) string {
	return appBaseURL() + "/verify-email?token=" + rawToken
}

func resetLink(rawToken string) string {
	return appBaseURL() + "/reset-password?token=" + rawToken
}
