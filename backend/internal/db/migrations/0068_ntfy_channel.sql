-- 0068_ntfy_channel.sql — the ntfy push-notification channel (third delivery
-- channel beside in-app + email). Two changes:
--
-- 1. users.ntfy_topic — the per-user delivery target (an ntfy topic name).
--    Empty = the ntfy channel is OFF for that user (mirrors "no email on file"
--    skipping the email channel). Default '' so existing users start opted-out.
--
-- 2. notification_prefs.channel CHECK — the prefs table is already keyed
--    (user, event_type, channel), but its channel CHECK only allowed
--    ('inapp','email'). Relax it to admit 'ntfy' so a user can mute the ntfy
--    channel per event type the same way they mute email. (The pref rows are
--    still opt-out: a row exists only to turn a channel off.)
ALTER TABLE users ADD COLUMN ntfy_topic TEXT NOT NULL DEFAULT '';

ALTER TABLE notification_prefs DROP CONSTRAINT notification_prefs_channel_check;
ALTER TABLE notification_prefs
  ADD CONSTRAINT notification_prefs_channel_check
  CHECK (channel IN ('inapp', 'email', 'ntfy'));
