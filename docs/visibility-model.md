# tela — Visibility & Sharing model (design note)

> Status: **shipped** (2026-06-03) — exposure model, indicators, audit view,
> and personal-space provisioning are all in. Remaining items are the explicit
> "Deferred" list below.
> Supersedes the implicit, three-mechanism status quo described below.

## Why

Today "who can see this page?" has **no single answer** — it's the sum of three
mechanisms that never reconcile in the UI:

1. **Space membership** (`owner`/`editor`/`viewer` in `space_members`) — all-or-nothing
   access to every page in a space. No per-page notion of privacy.
2. **Share links** (`/share/{token}`) — good, deliberate, per-page: password, expiry,
   include-descendants, revoke. This part stays.
3. **`/p/{id}` permalink** — an **always-on** public OG envelope for *every page that
   exists*. A crawler-UA request (no auth, no share link) returns the title, space
   name, a ~200-char body excerpt, and an OG image — for any sequential page id.

And the UI surfaces **none** of it: no badge on the page, no marker in the sidebar,
the Share sheet is editor-only, so the only way to learn a page's state is to open it.
Net effect: you can't trust it with personal notes (fear of leakage) *and* can't trust
it as a share surface (no confidence in what's exposed). That kills daily use.

## The model

Two independent axes. Each has **exactly one place** you look to answer it.

### Axis 1 — People (identity): lives on the **space**, never the page

A page has **no personal access list**. It inherits its space's locked-down member
set. "Who can open this internally?" is always answered by the page's *space*, the
same answer for every page in it.

- **Truly private to one person = a space with one member (you).** Privacy is not a
  page flag; it's a private space. → see "Frictionless personal space" below.
- No per-page user ACLs. (Deliberate: per-page identity sharing is exactly what made
  Google-Docs sprawl unknowable. Keeping identity at the space level is what makes the
  system answerable at a glance.)

> **Orgs (#153) keep this axis intact.** Identity access can be conferred to a
> *group* (an org) as well as an individual, but still **only at the space level** —
> never per-page. A space's "who can open this" is now: direct members ∪ members of
> any org the space is shared with, resolved in one place (the `space_access` view).
> Org grants are `editor`/`viewer` only; `owner` stays a direct-user responsibility.
> The answer to "who can see this page?" is still a single space-level lookup.

### Axis 2 — Public exposure (link): lives on the **page**

Every page resolves to one **public-exposure state**, computed from active share links
(its own, or an ancestor's `include_descendants` share) and shown ambiently:

| State | Icon | Means |
|---|---|---|
| **Space-only** (resting) | quiet `Space` chip (`Users`/`Lock`) | members of this page's space only |
| **Public link** | `Globe` | anyone with the link — no password |
| **Password link** | `KeyRound` | anyone with the link **+** password |
| **Inherited** | dimmed icon + `CornerDownRight` | exposed via a parent's "include children" share |

Icons are **Lucide** (the FE convention), not emoji — chosen for a clean, consistent
set. The resting **Space-only** chip is shown explicitly, so a missing icon never reads
as "didn't load." A page with multiple links collapses to the *most open* state (open >
password), with the rest visible in the manager.

> "Public" here = an open (no-password) share link. A future, distinct **Published**
> state (clean indexed URL, no token) is out of scope — see Deferred.

## Where each axis is surfaced

1. **Page-header visibility pill** — `🔒 Space` / `🌐 Public` / `🔑 Password`, next to
   the title. **Visible to every space member** (anyone can tell what's exposed).
   Clicking opens the share panel; **creating/revoking still requires editor+**.
   (Today the Share button is editor-only — viewers can't even *see* state. That flips:
   everyone sees state, only editors change it.)
2. **Sidebar marker** — a small icon on exposed pages so you can *scan the tree* and see
   your whole exposure surface at once. Inherited shares get the dimmed `↳` treatment.
3. **"Shared" audit view** — one screen listing everything reachable by link right now,
   across all spaces you can see, with state + expiry. The real antidote to "I'm never
   sure": you check a list instead of trusting memory. (Highest-trust item here.)
4. **Space members view** — the Axis-1 answer: a space clearly shows its locked-down
   member set (a one-member space reads unmistakably as "just you").

## Frictionless personal space (load-bearing)

If "private" means "a private space," then daily *personal writing* lives in a
one-member space — so that space must be **effortless and always there**, or we've
re-created capture friction. Today bootstrap creates no space at all
(`auth/bootstrap.go`).

Shipped: migration `0014_personal_spaces.sql` adds `spaces.personal_user_id`
(partial-unique). `api.EnsurePersonalSpace` idempotently creates a private,
one-member "Personal" space (owner = the user); it runs when the admin creates a
user and is backfilled for everyone (incl. the bootstrap admin) at startup via
`EnsurePersonalSpacesForAll` from `main.go`. "Ensure if missing", so a deleted
personal space returns on next boot — fine for a default home. **Personal
capture is now one click, not "create space → set membership → create page."**

## Implementation sketch (no core migration)

- **Backend:** enrich the page-list / tree payload (`api/pages.go`) with a resolved
  `exposure` per page: `{ state: "private"|"public"|"password", inherited: bool,
  expires_at }`. Computed from `share_links` (own + ancestor `include_descendants`),
  folded into the existing tree build. Read-only derived field; share CRUD is unchanged.
- **Frontend:** a `VisibilityBadge` owned primitive (tokens + Storybook story per the FE
  rules) used in the page header and, compact, in the sidebar. New route/panel for the
  audit view. Badge visible to all members; manager gated to editor+ as today.
- **No schema change** for the core. A future `Published` state may add a `pages`
  column; not now.

## Deferred (explicit, not forgotten)

- **`/p/{id}` leak** — narrow the always-on permalink to **title-only** OG (drop the body
  excerpt / image for non-shared pages), or fold it into a real Published state. Agreed
  to handle after this lands; not a blocker.
- **Published state** (🌐 clean indexed URL, no token).
- **Personal-space UI polish** — label/pin the personal space in the spaces list
  ("just you"); the space itself is provisioned, this is only affordance.

## Resolved (2026-06-03)

1. **Personal space — every user, auto.** Each user gets a private one-member space
   provisioned automatically. Capture works out of the box for everyone.
2. **Audit view — its own route/screen.** A dedicated "Shared" page across spaces.
3. **Sidebar markers — on by default.** Add a toggle later only if the tree feels noisy.
4. **Icons — Lucide, not emoji.** A clean, consistent set; no emoji in the UI.
