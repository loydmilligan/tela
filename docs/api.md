# tela — API

Base: `/api`. JSON in/out (imports are `multipart/form-data`). Auth is a session cookie (`tela_session`) **or** `Authorization: Bearer tela_pat_...`. Bearer is checked before the cookie; an invalid bearer returns 401 (no cookie fallback).

## Errors

Baseline envelope: `{ "error": "...", "code": "..." }`. Known codes: `bad_request`, `not_found`, `unauthorized`, `forbidden`, `conflict`, `cycle`, `last_admin`, `last_owner`, `internal`, `viewer_no_write`, `comment_*`, `revision_not_found`, `invalid_query`, `space_not_found`, `password_required`, `rate_limited`, `api_key_scope`, `api_key_space_scope`, `mira_password_required`.

Extended exception: `mira_password_required` is a 403 with a third field `{ error, code, unlock }` (`unlock` = upstream unlock URL). REST clients consume it; the MCP wrapper strips extra fields. When adding a code that carries extra wire fields, document it here **and** check the MCP `safeParseEnvelope`.

## Meta
- `GET /api/health` — liveness.
- `GET /api/version` — `{ version, commit, built_at }`, public, build-stamped.

## Auth
- `POST /api/auth/login` → 200 + cookie, or 401.
- `POST /api/auth/logout` → 204.
- `GET /api/auth/me` → current user.

Middleware bypasses `/api/health`, `/api/version`, `/api/auth/`, `/p/{id}`, `/share/{token}`, `/api/share/`, `/api/diagrams/`.

## Spaces & membership
- `GET /api/spaces` — spaces the caller can access (direct membership **or** via an org grant; resolved through the `space_access` view).
- `POST /api/spaces` — create (creator becomes owner).
- `GET|PATCH|DELETE /api/spaces/{id}`.
- `GET|POST|PATCH|DELETE` space members under the space (owner-gated; `last_owner` guard). These are **direct user** grants (`space_members`).

## Organizations (#153)
An org is a *grantable principal*: share a space with an org and every member gains the granted role. Access resolves through the `space_access` view = direct user grants ∪ org grants. Slot reserved for future `group` principals (same view, same routes).
- `GET /api/orgs` — caller's orgs (instance-admins see all; each row carries `my_role`, `member_count`).
- `POST /api/orgs` — create. **Instance-admin only.**
- `GET|PATCH|DELETE /api/orgs/{id}` — get/rename (org-admin or instance-admin) / delete (instance-admin only; tears down the org's space_grants).
- `GET|POST|PATCH|DELETE /api/orgs/{id}/members[/{user_id}]` — membership (org-admin or instance-admin; self-leave allowed; `last_admin` guard).
- `GET|POST|PATCH|DELETE /api/spaces/{id}/grants[/{grant_id}]` — share a space with an org. **Space owner only**; role limited to `editor`/`viewer` (`owner` reserved for direct users so the last-owner guard stays sound).
- `GET|POST|DELETE /api/admin/org-domains[/{domain}]` — auto-join email-domain → org mappings. **Instance-admin only.** A user whose verified email domain matches is enrolled into the org on verify/login (idempotent, best-effort).

## Pages
- `GET /api/spaces/{id}/pages` — pages in a space (optional `parent_id`).
- `POST /api/spaces/{id}/pages` — create.
- `GET /api/pages/{id}` — page (markdown body + metadata; envelope `{ page: ... }`). `?draft=$revId` for owner soft-draft.
- `PATCH /api/pages/{id}` — update title/body/parent/position; snapshots a revision on body/title change.
- `DELETE /api/pages/{id}` — soft delete.
- `GET /api/pages/{id}/revisions` — page history. (cross-page rev → 404 `revision_not_found`.)
- `GET /api/pages/{id}/backlinks` — pages linking here.
- `GET /api/pages/bodies?space_id&...` — bodies for the per-space fuzzy index.
- WebSocket `/ws/...` — live collab (custom 1-byte-tag protocol; see architecture.md).

## Search
- `GET /api/search?q=...` — FTS5 over title + body, snippet-highlighted.
- `GET /api/search/bodies?space_id&q&limit` — per-space body search (member-gated, bearer-`read` ok). Limit clamped to [1,100].

## Diagrams (Excalidraw)
- `GET /api/diagrams/{page_id}/{file}` — public, content-addressed, immutable (ETag/304).
- `PUT /api/pages/{id}/diagrams` — editor+, 8 MiB PNG (magic-byte checked), idempotent upsert.

## Import
- `POST /api/spaces/{id}/import` — editor+, `multipart/form-data`: `parent_id`, `dry_run`, `files` (`.md`/zip). Flatten-root + README-as-index handling.
- `POST /api/spaces/{id}/import-mira` — editor+, JSON `{ parent_id?, source_url? | payload? }` (exactly one of source_url/payload). URL fetch is https-only / allowlisted / no-redirect / capped.

## Public share
- Management (editor+, session): `POST|GET /api/pages/{id}/shares`, `PATCH|DELETE /api/shares/{id}`.
- Public (no session): `GET /api/share/{token}`, `POST /api/share/{token}/auth`, `GET /api/share/{token}/page/{page_id}`, `GET /api/share/{token}/tree`. Identical 404 for missing/revoked/expired. Rate-limited per (token, IP).

## API keys (instance-admin)
- `POST /api/api_keys` → 201 with the raw `key` **once** (`tela_pat_<43 chars>`); stored as HMAC.
- `GET /api/api_keys` → list (prefix only).
- `DELETE /api/api_keys/{id}` → soft-revoke (admin or owner), idempotent 204.
- `GET /api/api_keys/{id}/audit?limit&before` → bearer-auth request log (owner/admin), 30-day retention.

## Feedback
- `POST /api/feedback` — session OR bearer (any scope, incl. `read`). `{ subject, body }` (1–200 / 1–8000) → 201 `{ feedback: {...} }`. Write-only (no GET, no admin UI).
