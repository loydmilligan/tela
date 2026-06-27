-- 0052_plan_atlas_sources.sql — per-tier Atlas source cap.
--
-- max_atlas_sources is how many Atlas sources (a git repo or a Jira project that
-- Atlas keeps generated + refreshed) an account may keep live — the per-tier
-- limit behind Atlas, beside the resource quotas and max_llm_calls_per_month.
-- NULL = unlimited, same convention as every other max_* column. This is the
-- display/source-of-truth value the public catalog (GET /api/plans) and the
-- landing pricing mirror; hard enforcement at source-create time is a follow-up
-- (advisory until limits.go gates it), matching how the other cloud limits
-- started. Numbers are editable here — tuning a tier stays a data change.

ALTER TABLE plans ADD COLUMN max_atlas_sources INTEGER;  -- NULL = unlimited

UPDATE plans SET max_atlas_sources = 1    WHERE key = 'personal_free';
UPDATE plans SET max_atlas_sources = 5    WHERE key = 'personal_plus';
UPDATE plans SET max_atlas_sources = 1    WHERE key = 'org_free';
UPDATE plans SET max_atlas_sources = 20   WHERE key = 'org_team';
-- personal_unlimited (internal comp) and org_enterprise stay NULL = unlimited.
