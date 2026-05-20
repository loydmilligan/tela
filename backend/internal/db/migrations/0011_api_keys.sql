-- 0011_api_keys.sql
-- M16.A.1 API keys: bearer-token authentication for headless agents
-- (precursor to the Tela MCP server). Per-user keys, three scopes
-- (read/write/admin), optional single-space restriction.
--
-- Storage layout:
--   - Raw key is never persisted. The 43-char base64url body is hashed via
--     HMAC-SHA256 keyed by TELA_API_KEY_SECRET; only the hex of the HMAC is
--     stored. Bearer auth looks rows up by key_hmac (UNIQUE) — the unique
--     index lookup is the constant-time compare equivalent since we never
--     touch the raw secret.
--   - key_prefix is the first 8 chars of the raw key, kept for UI
--     identification so users can tell their keys apart in the management
--     list without re-displaying the secret.
--   - scope is one of read / write / admin, enforced at the API layer.
--   - space_id NULL means the key inherits the user's normal space-membership
--     visibility; non-NULL gates ALL page/comment access to that single
--     space.
--
-- Datetime columns are TEXT to keep the project-wide
-- 'YYYY-MM-DD HH:MM:SS' wire format. TIMESTAMP affinity would be
-- RFC3339-converted on read by modernc.org/sqlite, breaking the convention.
--
-- TELA_API_KEY_SECRET must be stable across deploys — regenerating it
-- invalidates every outstanding PAT because the HMACs no longer match.

CREATE TABLE api_keys (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  key_prefix TEXT NOT NULL,
  key_hmac TEXT NOT NULL,
  scope TEXT NOT NULL CHECK (scope IN ('read','write','admin')),
  space_id INTEGER REFERENCES spaces(id) ON DELETE CASCADE,
  last_used_at TEXT,
  expires_at TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S','now')),
  revoked_at TEXT
);

CREATE UNIQUE INDEX idx_api_keys_hmac ON api_keys(key_hmac);
CREATE INDEX idx_api_keys_user ON api_keys(user_id) WHERE revoked_at IS NULL;
