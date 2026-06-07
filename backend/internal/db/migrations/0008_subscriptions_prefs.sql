-- 0008_subscriptions_prefs.sql — follow page/space + per-user notification
-- preferences. See docs/notifications.md.
--
-- subscriptions: who follows what. Polymorphic subject (page|space), like
-- access_audit / notifications — no FK on subject_id, so deletes are cleaned up
-- by the page/space delete paths (DELETE FROM subscriptions …), not a cascade.
-- A page edit notifies followers of the page AND followers of its space.
--
-- notification_prefs: per (user, event_type, channel) on/off. Opt-out model —
-- absence of a row means enabled, so a new user gets everything by default and
-- a row is written only to turn something off. channel is 'inapp' (live now) or
-- 'email' (stored now, delivered when the email channel ships) — adding a
-- channel is data, not DDL.

CREATE TABLE subscriptions (
  user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  subject_kind TEXT   NOT NULL CHECK (subject_kind IN ('page','space')),
  subject_id   BIGINT NOT NULL,
  created_at   TEXT   NOT NULL DEFAULT tela_now(),
  PRIMARY KEY (user_id, subject_kind, subject_id)
);

CREATE INDEX idx_subscriptions_subject ON subscriptions(subject_kind, subject_id);

CREATE TABLE notification_prefs (
  user_id    BIGINT  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  event_type TEXT    NOT NULL,
  channel    TEXT    NOT NULL CHECK (channel IN ('inapp','email')),
  enabled    INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (user_id, event_type, channel)
);
