-- 0021_plan_features_trials.sql — per-plan feature flags + account trial window.
--
-- features: a JSONB map of feature-key → bool on each plan — the boolean
-- entitlement layer beside the numeric quotas (limits.go). featureEnabled()
-- consults it. The cloud-backed features (managed_rag, publishing) are flagged
-- on the paid tiers; on a self-host instance these are BYO and not plan-gated,
-- so the flags are advisory until the cloud-connect entitlement path reads them.
--
-- trial_plan_key / trial_ends_at: a 30-day trial of a paid tier auto-applied at
-- registration. planFor() resolves the EFFECTIVE plan — the trial tier while
-- trial_ends_at is in the future, else the base plan_key — so expiry is a
-- graceful, jobless downgrade (a stale trial_ends_at just stops winning the
-- CASE). No billing engine involved.

ALTER TABLE plans ADD COLUMN features JSONB NOT NULL DEFAULT '{}';

UPDATE plans SET features = '{"managed_rag": true, "publishing": true}'
  WHERE key IN ('personal_plus', 'personal_unlimited', 'org_team', 'org_enterprise');

ALTER TABLE users ADD COLUMN trial_plan_key TEXT REFERENCES plans(key);
ALTER TABLE users ADD COLUMN trial_ends_at  TEXT;  -- 'YYYY-MM-DD HH:MM:SS' UTC, NULL = no trial
