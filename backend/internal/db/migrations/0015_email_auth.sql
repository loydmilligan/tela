-- 0015_email_auth.sql
-- Email-first auth: self-registration + email confirmation + password reset.
--
-- Adds email identity to users (nullable so existing username-only rows and
-- the bootstrap admin keep working) and a token table backing the verify /
-- reset email links. Tokens are stored only as a SHA-256 hash — the raw token
-- lives solely in the emailed link, so a DB read never yields a usable token.
--
-- Email is stored lowercased; the partial UNIQUE index enforces
-- case-insensitive single-account-per-address while leaving legacy NULL-email
-- rows unconstrained.

ALTER TABLE users ADD COLUMN email TEXT;
ALTER TABLE users ADD COLUMN email_verified_at TEXT;

CREATE UNIQUE INDEX idx_users_email ON users(email) WHERE email IS NOT NULL;

CREATE TABLE email_tokens (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('verify','reset')),
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  consumed_at TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_email_tokens_user ON email_tokens(user_id);
CREATE INDEX idx_email_tokens_expires ON email_tokens(expires_at);
