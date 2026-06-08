# Metering & tiers

tela meters resource usage per **account** and enforces per-tier limits. This doc
covers the model, where it's enforced, and how to operate it (e.g. grant someone
an unlimited tier). The policy code is one file: `backend/internal/api/limits.go`.

## Model

An **account** is the thing a plan attaches to and that quotas charge against. There
are two kinds:

- **user** — a person's personal account (`users.plan_key`).
- **org** — an organization (`orgs.plan_key`).

Every space has an **owning account**, resolved by `spaceOwner()`:

1. `spaces.org_id` set → the **org** owns it.
2. else `spaces.personal_user_id` set → that **user** (their auto-provisioned
   personal home).
3. else (legacy "team" space, both NULL) → the **user** who holds the
   `space_members` `owner` row.

When a user creates a space they choose Personal or one of their orgs
(`POST /api/spaces` / MCP `create_space`, optional `org_id`). An org-owned space is
auto-shared with the whole org as editors (via `space_grants`); the human creator
stays the `space_members` owner (the no-principal-owner trigger forbids org owners).

## Plans

`plans` (migrations `0017_plans.sql`, `0018_plan_prices.sql`) is the **single source
of truth** for limit values — tuning a tier is a data change, not a deploy.

| column | meaning |
|---|---|
| `key` | stable id, e.g. `org_team` |
| `account_kind` | `user` \| `org` — which kind may hold this plan |
| `name` | display name |
| `max_spaces` | spaces the account may own (NULL = unlimited; a user's personal home is exempt) |
| `max_pages_per_space` | live pages within any one owned space (NULL = unlimited) |
| `max_storage_bytes` | sum of live attachment bytes across owned spaces (NULL = unlimited) |
| `max_members` | org seats (org plans only; NULL = unlimited) |
| `listed` | `0` = internal/comp tier hidden from the public catalog but still admin-assignable |
| `price_cents` / `price_period` | display pricing only — **there is no billing engine** |

`NULL` in any `max_*` means **unlimited**.

Seeded tiers: `personal_free`, `personal_plus`, `personal_unlimited` (unlisted comp),
`org_free`, `org_team`, `org_enterprise`.

## Enforcement points

All gates live in `limits.go` and return `*apiErr{402, "quota_exceeded", …}`, so REST
and MCP surface them identically (agents key on the `code`). They're injected at the
**shared cores**, so a new transport that reuses a core inherits the check.

| limit | gate | wired into |
|---|---|---|
| spaces | `checkSpaceQuota` | `createSpaceCore` (REST + MCP) |
| pages / space | `checkPageQuota` / `checkPageQuotaN` | `createPageCore`, `importMiraCore`, `ImportSpace` (n=files), cross-space `applyMoveTx` (n=subtree) |
| storage | `checkStorageQuota` | `UploadPageAttachment`, WebDAV file PUT (charges net-new bytes) |
| seats | `checkSeatQuota` | `AddOrgMember` |

Quotas gate **new growth only** — they never retroactively block reads/edits of
content that already exists. An account moved to a lower tier keeps what it has; it
just can't add more until under the new limit.

Checks run on `s.DB` just before the insert (a small, acceptable TOCTOU window for
soft caps). The counters take a `queryer`, so moving a check inside a caller's `tx`
for exactness is a drop-in change.

## Read & admin APIs

- `GET /api/usage` — caller's personal-account plan + live usage.
- `GET /api/orgs/{id}/usage` — an org's plan + usage (any member).
- `GET /api/plans` — the tier catalog (includes `listed=0` tiers; consumers filter).
- `PATCH /api/admin/plan` — **instance-admin only**, body `{account_kind, account_id, plan_key}`.

In the app: **Settings → Plan & Usage** shows usage bars + the tier catalog; the
admin **Users** and **Organizations** tabs carry an inline tier selector
(`PlanTierSelect`). The marketing prices live on the landing (`/pricing`).

## Operations

### Grant an account an unlimited tier

Preferred — via the API as an instance admin:

```
PATCH /api/admin/plan
{ "account_kind": "user", "account_id": 1, "plan_key": "personal_unlimited" }
```

(`org_enterprise` is the org-side unlimited tier.) Or directly on prod:

```bash
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U tela -d tela -c \
  "UPDATE users SET plan_key='personal_unlimited' WHERE username='someone';"
```

### Change a tier's limits or price

Edit the row in `plans` (a `UPDATE`, or a new forward-only migration for a durable
change). No re-deploy needed for a live data edit; the migration route keeps it
reproducible across environments.

### Add a new tier

Insert a `plans` row with the right `account_kind`. If it should appear in the public
catalog/landing, set `listed=1` and give it `price_cents`/`price_period`; otherwise
it's admin-assignable only. The landing pricing cards are hand-authored in
`landing/src/components/Pricing.astro` — update them to match if the public ladder
changes.
