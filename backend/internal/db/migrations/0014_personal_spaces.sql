-- 0014_personal_spaces.sql
-- Personal spaces: every user gets a private, one-member space as their default
-- home for personal writing (docs/visibility-model.md). Privacy is modelled as
-- "a space only you belong to", not a per-page flag — so the personal space is
-- where solo notes live.
--
-- personal_user_id labels which space is a given user's personal one; the
-- partial UNIQUE index guarantees at most one per user while allowing the many
-- NULLs of ordinary shared spaces. Access still lives entirely in space_members
-- like any other space — this column only drives provisioning idempotency (and
-- future UI affordances). ON DELETE SET NULL so removing a user demotes their
-- personal space to an ordinary orphan rather than cascade-deleting its pages.

ALTER TABLE spaces ADD COLUMN personal_user_id INTEGER
  REFERENCES users(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX idx_spaces_personal_user
  ON spaces(personal_user_id)
  WHERE personal_user_id IS NOT NULL;
