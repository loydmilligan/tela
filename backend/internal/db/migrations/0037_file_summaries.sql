-- 0037_file_summaries.sql — machine-generated summaries for attached files, the
-- file half of internal/summarize (sibling to page_summaries, migration 0030).
--
-- A page's summary lives in pages.props->>'summary'; a file has no props bag, so
-- the summary TEXT lives in a column on space_files directly (read by the file
-- card / list_attachments). file_summaries records HOW it was produced so the
-- worker tells fresh from stale, exactly as page_summaries does for pages.
--
-- src_hash here is the file's content_hash at generation time (NOT a hash of the
-- extracted text): content is immutable per upload and dedups by hash, so a
-- content change flips content_hash → the row reads stale and re-summarizes,
-- with no extraction needed to compute freshness. A non-text file (image, scanned
-- PDF, binary) records a row with an EMPTY summary + matching hash so the stale
-- sweep marks it done and never re-queues it. last_error/attempts carry the
-- failure state for the retry/backoff loop.

ALTER TABLE space_files ADD COLUMN summary TEXT NOT NULL DEFAULT '';

CREATE TABLE file_summaries (
  space_file_id BIGINT PRIMARY KEY REFERENCES space_files(id) ON DELETE CASCADE,
  src_hash      TEXT NOT NULL,
  model         TEXT NOT NULL,
  generated_at  TEXT NOT NULL DEFAULT tela_now(),
  last_error    TEXT NOT NULL DEFAULT '',
  attempts      INTEGER NOT NULL DEFAULT 0
);
