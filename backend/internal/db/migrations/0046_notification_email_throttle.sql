-- 0046_notification_email_throttle.sql — per-(user, page) throttle for
-- page_updated EMAILS. In-app page_updated already collapses to one unread row
-- per subject; email has no such collapse, so without a throttle a flurry of
-- edits to a followed page would fan out an email per edit. This table records
-- the last page_updated email sent to a user for a page; the dispatch path
-- atomically claims a send only when the window has elapsed (see
-- claimPageUpdatedEmail in notifications.go). Other event types (mention,
-- comment_reply, space_added) are one-shot and not throttled, so they never
-- touch this table.
--
-- subject_id is a page id (polymorphic-free: only page_updated uses this), no
-- FK — cleaned up by the page-delete path alongside notifications/subscriptions.
CREATE TABLE notification_email_throttle (
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  event_type TEXT   NOT NULL,
  subject_id BIGINT NOT NULL,
  sent_at    TEXT   NOT NULL DEFAULT tela_now(),
  PRIMARY KEY (user_id, event_type, subject_id)
);
