-- 0003_chunk_fts.sql — lexical half of hybrid chunk retrieval.
--
-- page_chunks (0002) is the vector half; this adds the keyword half over the
-- same rows so a single RRF fusion can blend them. Native Postgres FTS:
-- to_tsvector + GIN, ranked with ts_rank_cd. (ts_rank is not BM25 — fine for
-- phase 1; a BM25 extension stays an open option if eval shows it's needed.)
--
-- Generated STORED column so the tsvector is maintained automatically on every
-- INSERT/UPDATE of content — the indexer never has to compute or write it.

ALTER TABLE page_chunks
  ADD COLUMN content_tsv tsvector
  GENERATED ALWAYS AS (to_tsvector('english', content)) STORED;

CREATE INDEX idx_page_chunks_content_tsv ON page_chunks USING GIN (content_tsv);
