# Notifications

A small, extensible notification system: "something happened that a specific
user should know about." Designed so new **event types** and new **delivery
channels** are additive — no schema churn, no second source of truth.

Status: **v1 ships in-app delivery for page-body @-mentions.** Everything below
the "Extension points" line is wired to grow without rework.

## Model

One row per (recipient, event). A notification is generic over its subject so any
entity can be the thing-you're-notified-about — the same shape the `access_audit`
table uses for `target_kind`/`target_id`.

`notifications` (migration `0007_notifications.sql`):

| column | meaning |
|---|---|
| `id` | identity PK |
| `user_id` | **recipient** (FK users, cascade) |
| `type` | event code — `mention` today; `comment_reply`, `space_added`, … later. Text, not an enum, so adding a type is data, not DDL. |
| `actor_id` | who caused it (FK users, set-null). NULL = system. |
| `subject_kind` + `subject_id` | the primary entity — `('page', 42)`. Generic. |
| `space_id` | for the deep-link + access context (FK spaces, cascade). NULL for space-less events. |
| `data` | `jsonb` — a small denormalized render payload (page title, actor username) so the inbox renders with no N+1 and survives the source being edited/deleted. |
| `dedup_key` | nullable idempotency key. A partial unique index on `(user_id, dedup_key)` makes re-emits no-ops. |
| `read_at` | NULL = unread. |
| `created_at` | `tela_now()`. |

Indexes: `(user_id, created_at DESC)` for the inbox; partial `(user_id) WHERE
read_at IS NULL` for the unread badge; partial unique `(user_id, dedup_key)
WHERE dedup_key IS NOT NULL` for idempotency.

## Emit seam

A single internal entry point — `Server.emitNotifications(ctx, ...notificationInput)`
in `internal/api/notifications.go`:

- **Best-effort.** Inserts are `ON CONFLICT … DO NOTHING`; any error is logged,
  never surfaced. A notification failure must never roll back or fail the action
  that triggered it. Called *after* the triggering transaction commits.
- **Idempotent.** `dedup_key` (e.g. `mention:page:42:7`) means a user is
  notified at most once for a given (page, mention) no matter how many times the
  page is re-saved. Removes the need to diff old vs new mention sets.
- **Access-gated at emit time.** Recipients are filtered through `space_access`
  before a row is written — you are never notified about (and the `data` payload
  never leaks the title of) a page you can't see.

### Today's only call site: page-body mentions

Mentions are a structured token in canonical markdown — `[@Name](tela://user/{id})`
— so they parse reliably (`parseUserMentions`, mirroring `parseWikiLinks`).
`notifyPageMentions` runs post-commit in `createPageCore` and in the
body-changed branch of `updatePageCore` (so both REST and the MCP `update_page`
tool notify). It parses the body, drops the actor, filters to space members, and
emits one `mention` per recipient.

## Read / manage API

All caller-scoped (a user only ever sees their own rows):

- `GET  /api/notifications?limit=N` — recent, newest first (actor username joined, `data` inlined).
- `GET  /api/notifications/unread-count` — `{ count }` for the bell badge.
- `POST /api/notifications/{id}/read` — mark one read (404 if not yours).
- `POST /api/notifications/read-all` — mark all read.

Frontend: a bell in the app-shell header (`NotificationBell`) polls the unread
count (30s) and opens an inbox panel; rows deep-link from
`subject_kind`/`space_id`/`subject_id` and mark-read on click.

## Extension points (how it grows — no rework)

- **New event type** → pick a `type` string, add an emit call at its source, add
  a render case in the frontend `NotificationBell`. No migration.
- **Comment mentions / replies** → same `emitNotifications`, `subject_kind='comment'`,
  `dedup_key='mention:comment:{id}:{uid}'`. Drops in when the comment composer
  gains the mention picker.
- **New delivery channel (email, Slack)** → add a `deliver()` fan-out inside
  `emitNotifications` keyed on a future per-user `notification_prefs`. The emit
  call sites don't change. In-app is just the first channel.
- **Watch/subscribe** → a `subscriptions(user_id, subject_kind, subject_id)`
  table feeds recipient lists into the same emit path.
- **Real-time** → today the badge polls; swap to SSE/WS later behind the same
  read API.
