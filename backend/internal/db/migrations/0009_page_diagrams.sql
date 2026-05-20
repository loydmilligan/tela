-- 0009_page_diagrams.sql
-- M13.2 RichView: Excalidraw PNG sidecar storage (hybrid model — researcher #98).
--
-- One row per (page, scene_hash) tuple. The markdown body of a page may
-- contain one or more ` ```excalidraw\n{json}\n``` ` fences; the editor
-- computes a sha256-derived hash of the scene JSON and uploads a client-
-- rendered PNG snapshot here. Read-only viewers (including share-mode)
-- render an <img> pointing at the PNG instead of loading the Excalidraw
-- runtime — zero bundle cost for the read path.
--
-- UNIQUE(page_id, scene_hash) lets PUT be idempotent: re-uploading the same
-- scene is a no-op on second submit. FK CASCADE drops diagrams when their
-- page is deleted. Orphan-on-edit (scene_hash no longer referenced in body)
-- is deferred to M13.3 / a sweep task.
--
-- Datetime stored as TEXT to match the project-wide YYYY-MM-DD HH:MM:SS
-- convention (avoids modernc.org/sqlite's TIMESTAMP -> RFC3339 conversion;
-- see 0008_share_links.sql comment).

CREATE TABLE page_diagrams (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  page_id INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  scene_hash TEXT NOT NULL,
  png BLOB NOT NULL,
  byte_size INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE(page_id, scene_hash)
);

CREATE INDEX idx_page_diagrams_page ON page_diagrams(page_id);
