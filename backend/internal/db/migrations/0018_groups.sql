-- 0018_groups.sql
-- Group sub-teams (see docs/access-model.md §Groups). A group is the third
-- grantable principal: nest under an org, grant a space to a group, and every
-- group member gains the role through the space_access view. No new resolution
-- path — just a third leg in the view.

-- A group belongs to exactly one org and cannot span orgs. Name is unique within
-- the org (so "Engineering" is unambiguous there).
CREATE TABLE groups (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  org_id INTEGER NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  CHECK (length(name) BETWEEN 1 AND 200),
  UNIQUE (org_id, name)
);

CREATE INDEX idx_groups_org ON groups(org_id);

-- Group membership ⊆ org membership (enforced by triggers below).
CREATE TABLE group_members (
  group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (group_id, user_id)
);

CREATE INDEX idx_group_members_user ON group_members(user_id);

-- Containment invariant (1): you can only add an org member to that org's group.
CREATE TRIGGER group_members_require_org_member
BEFORE INSERT ON group_members
WHEN NOT EXISTS (
  SELECT 1 FROM org_members om
   JOIN groups g ON g.id = NEW.group_id
  WHERE om.org_id = g.org_id AND om.user_id = NEW.user_id
)
BEGIN
  SELECT RAISE(ABORT, 'group member must be a member of the group org');
END;

-- Containment invariant (2): leaving the org leaves all of its groups.
CREATE TRIGGER org_members_cascade_groups
AFTER DELETE ON org_members
BEGIN
  DELETE FROM group_members
   WHERE user_id = OLD.user_id
     AND group_id IN (SELECT id FROM groups WHERE org_id = OLD.org_id);
END;

-- Rebuild space_access with the group leg (SQLite views can't be ALTERed).
-- Effective access = direct user grants ∪ org-mediated ∪ group-mediated; the
-- effective role stays a pure max across all three.
DROP VIEW space_access;
CREATE VIEW space_access (space_id, user_id, role) AS
  SELECT space_id, user_id, role FROM space_members
  UNION ALL
  SELECT sg.space_id, om.user_id, sg.role
    FROM space_grants sg
    JOIN org_members om ON om.org_id = sg.principal_id
   WHERE sg.principal_kind = 'org'
  UNION ALL
  SELECT sg.space_id, gm.user_id, sg.role
    FROM space_grants sg
    JOIN group_members gm ON gm.group_id = sg.principal_id
   WHERE sg.principal_kind = 'group';
