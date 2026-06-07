-- 0007_pages_soft_delete.sql — soft-delete for pages (sync delete-safety).
--
-- A sync glitch must never hard-destroy wiki content, and ON DELETE CASCADE on
-- parent_id means one hard delete nukes a whole subtree. Pages now soft-delete
-- via deleted_at: deletePageCore stamps the row (and its descendants) instead of
-- DELETE, every read filters `deleted_at IS NULL`, and a re-synced file bearing a
-- trashed page's id can resurrect it (sync §6). Mirrors the comments precedent
-- (0001_init.sql) — nullable TEXT timestamp + partial indexes for the hot reads.

ALTER TABLE pages ADD COLUMN deleted_at TEXT;

-- The hot read path (space tree / sibling lookups) only ever wants live rows, so
-- make the covering index partial: smaller, and it never scans trashed rows.
DROP INDEX idx_pages_space_parent_pos;
CREATE INDEX idx_pages_space_parent_pos ON pages(space_id, parent_id, position) WHERE deleted_at IS NULL;
