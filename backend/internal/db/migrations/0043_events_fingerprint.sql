-- 0043_events_fingerprint.sql — grouping key for the admin client-error "Issues"
-- view.
--
-- client.error events (browser crash reports) are recorded into `events` like
-- everything else, but the Errors screen wants them GROUPED — the same error
-- firing 200 times should be one row with a count, not 200 feed entries. The
-- fingerprint is computed server-side at ingest (kind + normalized message +
-- first stack frame, see api/client_errors.go) so identical errors collapse even
-- when ids/numbers in the message differ. NULL for every other event type.
ALTER TABLE events ADD COLUMN fingerprint TEXT;

-- Partial index: the grouped query filters to client-error rows that carry a
-- fingerprint and aggregates by it. Keeps the index tiny (only error rows) while
-- covering GROUP BY fingerprint + the per-group recent-occurrences drill-down.
CREATE INDEX idx_events_fingerprint ON events(fingerprint, id DESC) WHERE fingerprint IS NOT NULL;
