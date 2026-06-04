-- 0017_orgs_lockdown.sql
-- Lock-down pass on the orgs access model (see docs/access-model.md):
--   1. Enforce the owner invariant at the DB layer (org/group grants can never
--      be 'owner' — owner is a direct-user-only responsibility).
--   2. Auto-join is member-only: drop org_email_domains.org_role.
--   3. access_audit: who-did-what trail for membership/grant/auto-join/org ops.

-- (1) Owner invariant — defense in depth behind the API checks. RAISE(ABORT)
-- rejects any attempt to write an 'owner' grant for a non-user principal,
-- including direct DB tampering or a future handler that forgets the rule.
CREATE TRIGGER space_grants_no_principal_owner_insert
BEFORE INSERT ON space_grants
WHEN NEW.principal_kind IN ('org', 'group') AND NEW.role = 'owner'
BEGIN
  SELECT RAISE(ABORT, 'org/group grants cannot have role owner');
END;

CREATE TRIGGER space_grants_no_principal_owner_update
BEFORE UPDATE ON space_grants
WHEN NEW.principal_kind IN ('org', 'group') AND NEW.role = 'owner'
BEGIN
  SELECT RAISE(ABORT, 'org/group grants cannot have role owner');
END;

-- (2) Auto-join is identity-derived and member-only. The per-domain role choice
-- is gone; matching users are always enrolled as 'member'. SQLite 3.35+
-- (modernc) supports DROP COLUMN.
ALTER TABLE org_email_domains DROP COLUMN org_role;

-- (3) Access audit. actor_user_id is NULL for system actions (auto-join). detail
-- is a short human-readable string (not parsed). Read surface is instance-admin.
CREATE TABLE access_audit (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  actor_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  action TEXT NOT NULL,
  target_kind TEXT NOT NULL,
  target_id INTEGER,
  detail TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_access_audit_created ON access_audit(created_at);
