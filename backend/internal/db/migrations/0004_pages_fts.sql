-- 0004_pages_fts.sql — ranked full-text search for the app's Tier-2 server search.
--
-- Replaces the unranked ILIKE placeholder in /api/search + /api/search/bodies
-- (the FTS5 bm25 ranking lost in the SQLite→Postgres switch). Page-level FTS,
-- distinct from page_chunks.content_tsv (0003), which is the RAG chunk index.
--
-- search_tsv is a generated STORED column so it's maintained automatically on
-- every title/body write — no app-side bookkeeping. Title is weighted 'A' (above
-- body 'B') so a title match outranks a body match (ts_rank_cd uses the weights).
--
-- Excalidraw fences are stripped in-SQL via regexp_replace so drawing JSON never
-- pollutes the lexical index — the SQL equivalent of the Go StripExcalidrawFences
-- used on the RAG side. Postgres regex: `.` matches newlines by default and `*?`
-- is non-greedy, so ```excalidraw … ``` blocks are removed whole. The 2-arg
-- to_tsvector('english', …) form is IMMUTABLE (required for a generated column);
-- regexp_replace / setweight / || are immutable too.

ALTER TABLE pages
  ADD COLUMN search_tsv tsvector
  GENERATED ALWAYS AS (
    setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
    setweight(to_tsvector('english', regexp_replace(coalesce(body, ''), '```excalidraw.*?```', '', 'g')), 'B')
  ) STORED;

CREATE INDEX idx_pages_search_tsv ON pages USING GIN (search_tsv);
