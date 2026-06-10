-- Per-user pinned spaces. A pin is just a (user, space) edge that floats a space
-- to a "Pinned" group at the top of the sidebar; visibility is still governed by
-- space_access, so the list read re-gates through it (mirrors favorites, 0006).
CREATE TABLE pinned_spaces (
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  space_id   BIGINT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL DEFAULT tela_now(),
  PRIMARY KEY (user_id, space_id)
);

CREATE INDEX idx_pinned_spaces_user ON pinned_spaces(user_id, created_at DESC);
