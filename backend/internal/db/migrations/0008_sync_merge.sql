-- 0008_sync_merge.sql — server-side 3-way merge support (sync §4/§5, Phase 4).
--
-- The merge needs the BASE text — what a given client last had — to diff3 the
-- incoming file against the current DB state (spec B5). We store that base blob
-- per (client, page): keyed on the api_key_id, since a stock WebDAV client
-- authenticates as its PAT and that is the only stable client identity we get
-- (one PAT per device; the tela-own engine will add a device id later). The base
-- is updated to whatever the client last exchanged — set to the served state on
-- a GET (download) and to the sent state on a PUT — so the next edit diffs
-- against the right ancestor. O(1) lookup via the composite primary key (the
-- perf bar). Both FKs cascade: revoking a key or hard-removing a page (rare —
-- pages soft-delete) drops the now-meaningless base rows.

CREATE TABLE sync_base (
  api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  page_id    BIGINT NOT NULL REFERENCES pages(id)    ON DELETE CASCADE,
  base_title TEXT  NOT NULL DEFAULT '',
  base_body  TEXT  NOT NULL DEFAULT '',
  base_props JSONB NOT NULL DEFAULT '{}',
  updated_at TEXT  NOT NULL DEFAULT tela_now(),
  PRIMARY KEY (api_key_id, page_id)
);

-- A merge whose overlapping hunks could not be auto-resolved auto-picks a side
-- (the local edit, by default) but stamps this so the UI can flag the page for
-- manual resolution; the overridden side is kept as a `sync-conflict` revision.
-- Cleared on the next clean manual save. Nullable TEXT timestamp, mirroring the
-- deleted_at / comments precedent. Not read by any page SELECT, so adding the
-- column is transparent to the existing scan paths.
ALTER TABLE pages ADD COLUMN sync_conflict_at TEXT;
