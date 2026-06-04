-- 0016_orgs.sql
-- Organizations (#153). Multi-tenant grouping layered on top of the existing
-- per-space membership model WITHOUT a tenant-isolation rewrite.
--
-- The load-bearing idea is the "grantable principal": access to a space can be
-- conferred to a *user* (space_members, unchanged) OR to an *org* (space_grants
-- with principal_kind='org'). A 'group' principal kind is reserved in the CHECK
-- so sub-teams can drop in later with zero schema churn — just a new leg in the
-- space_access view + grant rows with principal_kind='group'.
--
-- Effective access is resolved once, in the space_access VIEW: the union of
-- direct user grants and org-mediated grants. Every read path queries the view
-- instead of space_members, so adding principal kinds never touches handlers.
--
-- space_members stays the canonical direct-user-grant table: it keeps its FK +
-- ON DELETE CASCADE and the tested last-owner / self-leave safeguards. Only the
-- new (org, and later group) principals live in space_grants — a column whose
-- principal_id is polymorphic and therefore cannot carry a real FK; cleanup on
-- org delete is handled by ON DELETE CASCADE here is impossible for the grant,
-- so the org-delete handler removes its grants explicitly (and a defensive
-- DELETE runs in the same tx).

-- Tenant container.
CREATE TABLE orgs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  CHECK (length(name) BETWEEN 1 AND 200)
);

-- Org membership + the scoped admin tier. 'admin' manages the org's members,
-- grants, and settings; 'member' just belongs. A user may be in many orgs.
CREATE TABLE org_members (
  org_id INTEGER NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  org_role TEXT NOT NULL CHECK (org_role IN ('admin','member')),
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (org_id, user_id)
);

CREATE INDEX idx_org_members_user ON org_members(user_id);

-- Instance-admin-curated auto-join: a user whose verified email domain matches
-- is enrolled into org_id at org_role on verify/login. Domain is the bare host
-- ("acme.com"), stored lowercased; one row per domain (a domain maps to one
-- org).
CREATE TABLE org_email_domains (
  domain TEXT PRIMARY KEY,
  org_id INTEGER NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  org_role TEXT NOT NULL DEFAULT 'member' CHECK (org_role IN ('admin','member')),
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_org_email_domains_org ON org_email_domains(org_id);

-- Owning org for a space (namespacing / governance). Nullable: personal spaces
-- and legacy spaces stay org-less. Slug remains globally unique for now (the
-- in-app address-bar slug feature depends on it) — per-org slug namespacing is
-- a deliberate later step.
ALTER TABLE spaces ADD COLUMN org_id INTEGER REFERENCES orgs(id) ON DELETE SET NULL;

CREATE INDEX idx_spaces_org ON spaces(org_id);

-- Polymorphic grant edge. principal_kind='user' rows are NOT written here today
-- (users live in space_members); the slot exists so the model is uniform if we
-- ever consolidate. principal_kind='group' is reserved for sub-teams.
CREATE TABLE space_grants (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  space_id INTEGER NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  principal_kind TEXT NOT NULL CHECK (principal_kind IN ('user','org','group')),
  principal_id INTEGER NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('owner','editor','viewer')),
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (space_id, principal_kind, principal_id)
);

CREATE INDEX idx_space_grants_space ON space_grants(space_id);
CREATE INDEX idx_space_grants_principal ON space_grants(principal_kind, principal_id);

-- Single source of truth for "who can access which space, at what role".
-- Resolution = direct user grants ∪ org-mediated grants. A user reachable via
-- several principals appears once per principal; callers that need the single
-- effective role pick the max by precedence (owner > editor > viewer), and pure
-- access gates use DISTINCT space_id. Add a leg here for 'group' later.
CREATE VIEW space_access (space_id, user_id, role) AS
  SELECT space_id, user_id, role FROM space_members
  UNION ALL
  SELECT sg.space_id, om.user_id, sg.role
    FROM space_grants sg
    JOIN org_members om ON om.org_id = sg.principal_id
   WHERE sg.principal_kind = 'org';
