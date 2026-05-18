-- 0001_init.sql
-- Initial schema for tela: spaces + nested pages.
-- Embedded into the backend binary via //go:embed in the db package.

CREATE TABLE spaces (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE pages (
  id INTEGER PRIMARY KEY,
  space_id INTEGER NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  parent_id INTEGER NULL REFERENCES pages(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_pages_space_parent_pos ON pages(space_id, parent_id, position);
