-- 0023_cloud_usage.sql — per-account managed-compute metering + monthly cap.
--
-- The rate limiter (auth_ratelimit.go) bounds BURST; this bounds TOTAL LLM usage
-- per calendar month, so an entitled account (or a farmed trial) can't run paid
-- AI compute up to an unbounded bill. Only LLM calls are metered: they're the
-- expensive, user-initiated ops (one per ask/chat). Embeddings are bulk
-- (indexing) and cheap — burst-limited only, NOT monthly-capped, so indexing a
-- space is never throttled by this.
--
-- max_llm_calls_per_month NULL = unlimited (the comp / enterprise tiers). The
-- numbers are tunable data, not code — mirrors the other plan limits.

ALTER TABLE plans ADD COLUMN max_llm_calls_per_month INTEGER;

UPDATE plans SET max_llm_calls_per_month = 50   WHERE key IN ('personal_free', 'org_free');
UPDATE plans SET max_llm_calls_per_month = 1000 WHERE key = 'personal_plus';
UPDATE plans SET max_llm_calls_per_month = 2000 WHERE key = 'org_team';
-- personal_unlimited and org_enterprise stay NULL (unlimited).

-- One row per (account, calendar-month). Incremented atomically per LLM call.
CREATE TABLE cloud_usage (
  account_kind TEXT    NOT NULL CHECK (account_kind IN ('user','org')),
  account_id   BIGINT  NOT NULL,
  period       TEXT    NOT NULL,            -- 'YYYY-MM' UTC
  llm_calls    INTEGER NOT NULL DEFAULT 0,
  updated_at   TEXT    NOT NULL DEFAULT tela_now(),
  PRIMARY KEY (account_kind, account_id, period)
);
