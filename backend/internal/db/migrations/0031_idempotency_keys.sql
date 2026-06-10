-- 0031_idempotency_keys.sql — make MCP create-type writes safe to retry.
--
-- After a dropped connection an agent can't tell whether a create_page (or
-- create_space / add_comment / import_mira) landed; retrying then duplicates.
-- A client-supplied idempotency_key lets the server dedupe: the FIRST call for a
-- (user, key) claims the row (result NULL = in-flight), runs the write, and
-- stores the structured result; a REPLAY with the same key returns that stored
-- result instead of creating a second row. The UNIQUE (user_id, idem_key) key is
-- the concurrency backstop — a racing duplicate loses the INSERT and either
-- replays the stored result or gets a transient "in progress" error.
--
-- result is the JSON of the tool's structured output (the {page}/{space}/...
-- envelope); NULL while the claiming call is still executing. tool pins the key
-- to one tool so a reused key on a different tool is rejected, not mis-replayed.
-- Keys accumulate (no TTL yet) — rows are tiny; prune by created_at later if
-- volume warrants.

CREATE TABLE idempotency_keys (
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  idem_key   TEXT   NOT NULL,
  tool       TEXT   NOT NULL,
  result     TEXT,                                  -- JSON structured output; NULL while in-flight
  created_at TEXT   NOT NULL DEFAULT tela_now(),
  PRIMARY KEY (user_id, idem_key)
);

CREATE INDEX idx_idempotency_keys_created ON idempotency_keys(created_at);
