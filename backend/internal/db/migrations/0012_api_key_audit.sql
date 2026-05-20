-- 0012_api_key_audit.sql
-- M16.A.2 API-key audit log. One row per bearer-authed request so we can
-- trace token activity after the fact and report on it from the Settings →
-- API Keys tab. Rows live for TELA_API_KEY_AUDIT_DAYS days (default 30,
-- swept every 6h by auth.StartAuditGC).
--
-- ts is TEXT not TIMESTAMP per the project-wide pitfall: modernc.org/sqlite
-- RFC3339-converts the TIMESTAMP affinity, which breaks the canonical
-- 'YYYY-MM-DD HH:MM:SS' wire format every other Tela datetime column uses.
--
-- The composite (api_key_id, ts) index lets the GET /api/api_keys/{id}/audit
-- read serve directly off the index (DESC scan + LIMIT) without re-sorting.
-- ON DELETE CASCADE on api_key_id means revoking a key (which only sets
-- revoked_at — soft delete) keeps the audit rows intact; the rows only
-- disappear if the parent api_keys row is hard-deleted, which currently only
-- happens via users CASCADE on account deletion. That is the right posture:
-- the audit trail outlives revocation, but doesn't outlive the user.

CREATE TABLE api_key_audit (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  api_key_id INTEGER NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  method TEXT NOT NULL,
  path TEXT NOT NULL,
  status_code INTEGER NOT NULL,
  ts TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S','now'))
);

CREATE INDEX idx_api_key_audit_key_ts ON api_key_audit(api_key_id, ts);
