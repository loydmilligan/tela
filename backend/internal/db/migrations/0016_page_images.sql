-- 0016_page_images.sql
-- Image-upload storage. Mirrors 0009_page_diagrams.sql: content-addressed
-- BLOBs in SQLite, served public + immutable. The editor uploads a pasted /
-- dropped image, gets back a `/api/images/{page_id}/{hash}.{ext}` URL, and
-- inserts standard `![](url)` markdown — so the body stays canonical markdown
-- (no proprietary block) and the bytes live alongside the DB like diagrams.
--
-- content_hash is a sha256 of the bytes (hex). UNIQUE(page_id, content_hash)
-- makes re-uploading the same image idempotent and lets the GET route be
-- content-addressed (Cache-Control: immutable). FK CASCADE drops images when
-- the page is deleted. Orphan-on-edit (image no longer referenced in body) is
-- deferred to a sweep task, exactly like diagrams.
--
-- mime is the server-detected image type (png/jpeg/gif/webp only; never
-- trusted from the client). Datetime TEXT per the project convention.

CREATE TABLE page_images (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  page_id INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  content_hash TEXT NOT NULL,
  mime TEXT NOT NULL,
  data BLOB NOT NULL,
  byte_size INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE(page_id, content_hash)
);

CREATE INDEX idx_page_images_page ON page_images(page_id);
