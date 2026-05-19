-- 0006_comments.sql
-- M8.0 Comments: SQL-only, fully decoupled from pages.body and Yjs.
--
-- Anchors are text-fingerprint strings ({prefix, exact, suffix}) — never
-- ProseMirror positions or Yjs RelativePositions. This survives page
-- rewrites, Yjs room GC, and the storage-canonical-markdown rule.
--
-- Threading is flat (depth-1): roots have parent_id IS NULL and carry
-- anchor_* fields; replies set parent_id to a root.id and ignore anchor_*.
-- Resolve toggles the root only.
--
-- Soft-delete via deleted_at; partial indexes filter deleted rows for the
-- two hot read paths (per-page list, per-root reply lookup).

CREATE TABLE comments (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  page_id         INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  parent_id       INTEGER REFERENCES comments(id) ON DELETE CASCADE,
  author_id       INTEGER NOT NULL REFERENCES users(id),
  body            TEXT NOT NULL,
  anchor_prefix   TEXT,
  anchor_exact    TEXT,
  anchor_suffix   TEXT,
  resolved        INTEGER NOT NULL DEFAULT 0,
  resolved_at     TEXT,
  resolved_by     INTEGER REFERENCES users(id),
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  deleted_at      TEXT
);

CREATE INDEX idx_comments_page ON comments(page_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_comments_parent ON comments(parent_id) WHERE deleted_at IS NULL;
