# Notifications

A small, extensible notification system: "something happened that a specific
user should know about." Designed so new **event types** and **delivery
channels** are additive — no schema churn, no second source of truth.

Status (v1, in-app):
- **@-mention** on a page → the mentioned member is notified.
- **Follow a page or space** → its `page_updated` edits notify you.
- **Preferences** — turn any event type off per channel.
- Delivery is **in-app only** today; the email channel is wired through prefs but
  not yet delivered.

## Tables

**`notifications`** (`0007`) — one row per (recipient, event). Generic over its
subject (`subject_kind`/`subject_id`, like `access_audit`) and its `type` (text,
not an enum), so a new kind is data, not DDL.

| column | meaning |
|---|---|
| `user_id` | recipient (FK users, cascade) |
| `type` | `mention`, `page_updated`, … |
| `actor_id` | who caused it (FK users, set-null) |
| `subject_kind` + `subject_id` | the entity — `('page', 42)` |
| `space_id` | deep-link + access context (FK spaces, cascade) |
| `data` | `jsonb` denormalized render payload (page title, actor name) — renders with no N+1, survives the source changing |
| `dedup_key` | nullable idempotency key; partial-unique on `(user_id, dedup_key)` |
| `read_at` | NULL = unread |

**`subscriptions`** (`0008`) — who follows what. Polymorphic
`(user_id, subject_kind ∈ page|space, subject_id)`. No FK on `subject_id`, so the
page/space delete paths clear them explicitly (notifications, which carry
`space_id`, cascade on space delete; a page delete clears both by hand).

**`notification_prefs`** (`0008`) — `(user_id, event_type, channel, enabled)`.
**Opt-out**: absence of a row means enabled, so a new user gets everything and a
row is written only to turn something off. `channel ∈ inapp | email`.

## Emit seam

One entry point — `Server.emitNotifications(ctx, ...notificationInput)`. It is
**best-effort** (errors logged, never surfaced; called after the triggering tx
commits), **preference-gated** (skips a recipient who turned the event type off
in-app), and recipients are **access-gated** before the call (never notify about,
or leak the title of, something you can't see). Three emission policies on
`notificationInput`:

- **`DedupKey`** → one-ever per `(user, key)` via `ON CONFLICT DO NOTHING`. For
  one-shot events: a mention (`mention:page:{id}:{uid}`).
- **`CollapseUnread`** → at most one *unread* per `(user, type, subject)`; once
  read, the next event makes a fresh row. For recurring events (a followed page
  changed) so a flurry of edits doesn't pile up.
- neither → always insert.

### Emit sites

- **Mentions** — `parseUserMentions` over `tela://user/{id}` in the page body
  (mirrors `parseWikiLinks`), wired post-commit into `createPageCore` +
  `updatePageCore`, so REST and the MCP `update_page` tool both notify.
- **page_updated** — on a body/title change, `notifyPageUpdate` notifies
  followers of the page *and* of its space (minus the editor), `CollapseUnread`.
- **Author auto-follow** — creating a page subscribes its author, so they hear
  about later edits without an explicit follow.

## API

Notifications (caller-scoped): `GET /api/notifications`,
`GET /api/notifications/unread-count`, `POST /api/notifications/{id}/read`,
`POST /api/notifications/read-all`.

Follow: `GET|POST|DELETE /api/pages/{id}/subscription` and the `…/spaces/{id}/…`
counterparts (viewer+ gated).

Preferences: `GET /api/users/me/notification-prefs` (full matrix, defaulting
enabled), `PUT /api/users/me/notification-prefs` (`{event_type, channel,
enabled}`).

Frontend: a header **bell** (polled unread badge + inbox panel), a **follow**
toggle in the page header, and a **Notifications** settings tab (the event ×
channel matrix).

## Extension points (additive — no rework)

- **New event type** → add a `notif*` const + emit call + a frontend render case
  + a row in the settings matrix. No migration.
- **Comment mentions / replies** → same seam, `subject_kind='comment'`. Drops in
  when the comment composer gains the mention picker.
- **New page in a followed space** → a `page_created` type emitted to space
  followers on create (deliberately not done yet to keep `page_updated` = edits).
- **Email channel** → a `deliver()` fan-out in `emitNotifications` reading the
  `email` prefs already stored. The emit sites don't change.
- **Realtime** → today the badge polls; swap to SSE/WS behind the same read API.
