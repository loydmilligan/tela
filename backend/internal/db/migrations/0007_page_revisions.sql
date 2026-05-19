-- 0007_page_revisions.sql
-- M9.0 PageHistory: per-save markdown revisions for every page.
--
-- One row per persisted save that actually changed body or title (no-op
-- PATCHes do NOT snapshot). Both manual saves (solo) and leader-rebase
-- writes go through the same PATCH /api/pages/{id} path and both write
-- source='manual' for now — split later if we ever need the distinction.
--
-- author_id is nullable on purpose: leader-rebase writes from a peer who
-- has since left could theoretically have a stale author; manual saves
-- always set it.
--
-- byte_size caches length(body) so the list view can render size without
-- pulling the full body column.

CREATE TABLE page_revisions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  page_id     INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  body        TEXT NOT NULL,
  title       TEXT NOT NULL,
  author_id   INTEGER REFERENCES users(id),
  source      TEXT NOT NULL,
  byte_size   INTEGER NOT NULL,
  created_at  TEXT NOT NULL
);

CREATE INDEX idx_page_revisions_page_created ON page_revisions(page_id, created_at DESC);

-- FUTURE pruning option: keep N=100 rows OR 90 days, whichever wider.
-- 2026-05-19 keeps revisions forever per Q28-A; revisit if storage ever bites.
