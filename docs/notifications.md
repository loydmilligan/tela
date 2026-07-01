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
commits) and recipients are **access-gated** before the call (never notify about,
or leak the title of, something you can't see). It fans each input out to every
enabled **channel**, gated **independently**: `insertInApp` writes the inbox row
when in-app is on, then `dispatchEmails` (see [Email channel](#email-channel))
sends when email is on — muting one channel never mutes the other. Three
emission policies on `notificationInput`:

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
- **page_created** — on create, `notifyPageCreated` notifies followers of the
  **space** (minus the author), so "follow a space" means "watch for new pages",
  not just edits. Idempotent per (page, user). Fires only on the interactive
  create path (`createPageCore`) — bulk import/sync doesn't storm followers.
- **user_registered** — when a new account **confirms its email** (first time
  only; the verify handler gates on the prior `email_verified_at` being NULL),
  `notifyUserRegistered` notifies every active **instance admin** (`is_instance_admin`)
  so an operator sees who's signing up. `subject_kind='user'`, the registrant is
  the actor; `DedupKey` makes it one-ever per (admin, new user). The Settings
  matrix row ("New signups") renders for admins only; non-admins never receive it.
- **atlas_run** — when an Atlas generation run reaches a terminal state,
  `notifyAtlasRunFinish` (`atlas_notify.go`) notifies the project's **managers**
  (owner user, or the org's admins). `subject_kind='space'` pointing at the output
  space (`subject_id=0` when none was materialized); `DedupKey` is one-ever per
  (manager, run, terminal status). The row's copy is pre-rendered server-side into
  `data.title`/`data.summary` (headline + coverage/stats or the failure message),
  and the frontend deep-links to the project via `data.project_id`.
- **Autowatch** — you auto-follow a page when you **create**, **edit**, or
  **comment** on it (Confluence-style), via `autoFollow` in the shared cores
  (`createPageCore` / `updatePageCore` / `createCommentCore`) so REST *and* MCP
  are covered uniformly. Gated by the per-user **`users.autowatch`** preference
  (default on; `GET|PUT /api/users/me/autowatch`, toggle in Settings →
  Notifications). Edit-autowatch is the interactive path only — sync edits go
  through `applyUpdateTx`, so a vault sync never auto-subscribes you.

## API

Notifications (caller-scoped): `GET /api/notifications`,
`GET /api/notifications/unread-count`, `POST /api/notifications/{id}/read`,
`POST /api/notifications/read-all`.

Follow: `GET|POST|DELETE /api/pages/{id}/subscription` and the `…/spaces/{id}/…`
counterparts (viewer+ gated). `GET /api/users/me/subscriptions` lists everything
the caller follows (pages + spaces, resolved to titles, access-gated) for the
management list.

Preferences: `GET /api/users/me/notification-prefs` (full matrix, defaulting
enabled), `PUT /api/users/me/notification-prefs` (`{event_type, channel,
enabled}`).

Frontend: a header **bell** (polled unread badge + inbox panel), a **follow**
toggle (the bell icon) in **both** the page header and the space header, a
**Notifications** settings tab (the event × channel matrix), and a **Following**
settings tab (the watch list, with one-click unfollow).

## Email channel

`dispatchEmails` (`notifications_email.go`) delivers every event type
out of the app, reusing the feedback pattern: recipient/content resolved
synchronously (ctx live), SMTP fired in a detached goroutine so relay latency
never slows the request. A missing relay (LogMailer) just logs. Needs
`TELA_SMTP_*` set (else log-only).

- **Gate** — `emailEnabled` mirrors `inAppEnabled` on the `email` channel of
  `notification_prefs` (opt-out: no row = enabled). No schema change — the column
  was already in the prefs matrix.
- **Recipient** — `users.email`; rows with no email (legacy username-only) are
  skipped.
- **Content** — branded HTML + plaintext via `mailer.NotificationMessage`,
  rendered through one shared `html/template` layout (accent bar, header with an
  event badge, workspace breadcrumb, footer panel). The **actor anchors the
  email**: an identity row (deterministic colored monogram chip + name + muted
  action), with the **page/space title as the clickable hero**. Mention and
  comment-reply carry a **snippet** (the mention's surrounding line / the reply
  body, flattened + truncated). **`page_updated` carries a "what changed" diff**
  — `changePreview` (`notifications_diff.go`) runs a bounded line-level LCS over
  the pre/post body (passed straight from the edit, no re-query), rendering up to
  6 changed lines (green `+`/red `−`) with an additions/deletions stat and an
  overflow note; oversized bodies fall back to stat-only. The diff is email-only
  (carried on `notificationInput.ChangeLines`, not persisted to the inbox row).
  Mention emails also append a **"Related in this wiki"** block from
  `rag.RelatedPages` (recipient-scoped; empty when the page is unindexed —
  graceful degrade). Links resolve to the org custom domain
  via `shareOriginForPage`/`shareOrigin`; the footer links to
  `/settings?tab=notifications` to manage.
- **page_updated throttle** — in-app collapses to one unread per subject; email
  has no collapse, so `claimPageUpdatedEmail` atomically caps it to **one email
  per (user, page) per `pageUpdatedEmailWindow`** (4h) via the
  `notification_email_throttle` table (migration `0046`, swept on page delete).
  The other types are one-shot, never throttled.
- **Slack/Teams later** — add a sibling `dispatch*` off the same loop + a channel
  value in the prefs `CHECK`; the emit sites don't change.

## Extension points (additive — no rework)

- **New event type** → add a `notif*` const + emit call + a frontend render case
  + a row in the settings matrix. No migration.
- **Comment mentions / replies** → same seam, `subject_kind='comment'`. Drops in
  when the comment composer gains the mention picker.
- **Realtime** → today the badge polls; swap to SSE/WS behind the same read API.
