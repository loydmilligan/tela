-- 0017_plans.sql — metering & tiers. Plans are the single source of truth for
-- per-account resource limits; both account kinds (a user's personal account and
-- an org) carry one. Enforcement lives in the API (internal/api/limits.go); this
-- table only holds the numbers, so tuning a tier is a data change, not a deploy.
--
-- A space's *owning account* is: its org (spaces.org_id, activated by this
-- feature) → else its personal_user_id → else the space_members owner (legacy
-- team spaces). Quotas (spaces / pages-per-space / storage) charge that account;
-- seat quotas charge the org. NULL in any max_* column means "unlimited".

CREATE TABLE plans (
  key                 TEXT PRIMARY KEY,
  account_kind        TEXT NOT NULL CHECK (account_kind IN ('user','org')),
  name                TEXT NOT NULL,
  -- NULL = unlimited for every limit below.
  max_spaces          INTEGER,   -- spaces the account owns (a user's personal home is exempt)
  max_pages_per_space INTEGER,   -- live pages within any one owned space
  max_storage_bytes   BIGINT,    -- sum of live attachment bytes across owned spaces
  max_members         INTEGER,   -- org seats (org plans only; ignored for user plans)
  -- listed=0 keeps a tier out of the public catalog (GET /api/plans, landing
  -- pricing) while still being assignable by an admin — for internal/comp tiers
  -- like an unlimited grant. listed tiers are the purchasable ladder.
  listed              INTEGER NOT NULL DEFAULT 1,
  created_at          TEXT NOT NULL DEFAULT tela_now(),
  updated_at          TEXT NOT NULL DEFAULT tela_now()
);

-- Seed tiers. Numbers are deliberately editable here; see docs / the create
-- defaults. 100MB = 104857600, 1GB = 1073741824, 5GB = 5368709120,
-- 50GB = 53687091200. personal_unlimited is unlisted (internal comp tier).
INSERT INTO plans (key, account_kind, name, max_spaces, max_pages_per_space, max_storage_bytes, max_members, listed) VALUES
  ('personal_free',      'user', 'Free',          3,    100,  104857600,    NULL, 1),
  ('personal_plus',      'user', 'Plus',          25,   1000, 5368709120,   NULL, 1),
  ('personal_unlimited', 'user', 'Unlimited',     NULL, NULL, NULL,         NULL, 0),
  ('org_free',           'org',  'Free',          10,   500,  1073741824,   5,    1),
  ('org_team',           'org',  'Team',          100,  NULL, 53687091200,  50,   1),
  ('org_enterprise',     'org',  'Enterprise',    NULL, NULL, NULL,         NULL, 1);

-- Every account references a plan. Defaults give existing users/orgs the free
-- tier of their kind on migrate. FK keeps plan_key honest; ON DELETE/UPDATE left
-- default (RESTRICT) so a referenced plan can't vanish out from under accounts.
ALTER TABLE users ADD COLUMN plan_key TEXT NOT NULL DEFAULT 'personal_free' REFERENCES plans(key);
ALTER TABLE orgs  ADD COLUMN plan_key TEXT NOT NULL DEFAULT 'org_free'      REFERENCES plans(key);
