-- 0002_search.sql
-- FTS5 search index over pages: title + body.
-- External-content table (content='pages') kept in sync via triggers.

CREATE VIRTUAL TABLE pages_fts USING fts5(
  title,
  body,
  content='pages',
  content_rowid='id',
  tokenize='porter unicode61'
);

CREATE TRIGGER pages_ai AFTER INSERT ON pages BEGIN
  INSERT INTO pages_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;

CREATE TRIGGER pages_ad AFTER DELETE ON pages BEGIN
  INSERT INTO pages_fts(pages_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
END;

CREATE TRIGGER pages_au AFTER UPDATE OF title, body ON pages BEGIN
  INSERT INTO pages_fts(pages_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
  INSERT INTO pages_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;

-- Backfill: FTS5 'rebuild' command re-indexes from the external content
-- table. Idempotent — safe even if the FTS table is already populated.
INSERT INTO pages_fts(pages_fts) VALUES('rebuild');
