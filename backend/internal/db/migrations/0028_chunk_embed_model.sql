-- 0028_chunk_embed_model.sql — stamp each chunk with the embedder that produced
-- its vector.
--
-- The chunk content_hash already folds in the model name, so a model change is
-- self-invalidating for the CACHE. This column is for OBSERVABILITY and future
-- zero-downtime model migration: it lets the index report "how many chunks are
-- still on the old model" (model drift) and, later, lets a new model be
-- backfilled into a parallel tag and swapped without a search-dark window.
--
-- Additive + forward-only. Existing rows get '' (legacy / unknown vintage); they
-- are re-stamped with the live model the next time their page is reindexed, and
-- '' is treated as "not drift" so a one-time backfill isn't forced.

ALTER TABLE page_chunks ADD COLUMN embed_model TEXT NOT NULL DEFAULT '';
