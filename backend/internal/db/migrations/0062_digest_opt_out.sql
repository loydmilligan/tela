-- 0062_digest_opt_out.sql — make the weekly digest opt-OUT for new accounts.
-- New users now default to 'weekly' (they can unsubscribe in one click). The
-- empty-digest skip means a dormant account still never gets a hollow email, so
-- opt-out stays quiet for people with nothing happening in their spaces.
-- Existing rows are intentionally NOT bulk-flipped here: turning the current
-- user base on is a real outward send and is done deliberately (tela digest
-- enable / a separate ops step), not as a schema migration side effect.
ALTER TABLE users ALTER COLUMN digest_frequency SET DEFAULT 'weekly';
