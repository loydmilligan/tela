-- 0006_favorites.sql — per-user page favorites (starred pages).
--
-- A favorite is a plain (user, page) edge: the user who starred a page can pull
-- it up from the sidebar / home dashboard. No role of its own — visibility is
-- still governed by space_access, so the favorites list is always re-gated on
-- read (a favorite to a page you've lost access to silently drops out).
--
-- Composite PK makes (un)starring idempotent (ON CONFLICT DO NOTHING). Both FKs
-- cascade so deleting a user or a page cleans up its favorites automatically.

CREATE TABLE favorites (
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  page_id    BIGINT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL DEFAULT tela_now(),
  PRIMARY KEY (user_id, page_id)
);

CREATE INDEX idx_favorites_user ON favorites(user_id, created_at DESC);
