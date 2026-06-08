# Org custom domains

A subdomain an org controls (e.g. `tela.ngss.io`) becomes that org's **white-label
front door**: for its users, that hostname *is* tela — login, private spaces,
editing, sharing, all served by the existing app under their brand. tela's own
marketing/docs/MCP/sync stay on the canonical host. Fully dynamic and multi-org:
orgs self-register hostnames at runtime and Caddy on-demand-issues each cert — no
per-domain config, no redeploy.

This is the **app-only** surface and is `noindex`. The public/SEO surface
(`/{handle}`, `/public`, `/u`, `/discover`, `sitemap`) stays canonical-only — see
[public-spaces.md](public-spaces.md).

## What a custom domain governs (and what it doesn't)

- **Governs:** branding (the org name on the login screen + auth shell), which
  sign-in methods show, and the **origin** of three browser-facing surfaces —
  auth emails, the org SSO callback, and share URLs.
- **Does NOT govern content visibility.** The space list is identity-based
  (`space_access`/grants), never filtered by host. A user sees exactly the spaces
  they can access regardless of which front door they came in through —
  deliberately, so a user with personal spaces or spaces shared from another org
  never "loses" them on a custom domain.

## Data model

- `org_hostnames` (migration `0024`): `hostname` PK → `org_id`, `status`
  (`pending`|`active`), `verify_token` (DNS-TXT challenge), `verified_at`. A
  hostname maps to exactly one org. Subdomain-only — `normalizeHostname` rejects a
  registrable apex via `golang.org/x/net/publicsuffix` (an apex can't `CNAME`
  anyway). `status='active'` is what makes a host real: it gates cert issuance,
  host→org resolution, and the white-label surface.
- `sessions.org_id` (migration `0025`): the org front door a session was created
  under (NULL = canonical). Enforces the session↔org binding.
- `org_login_settings` (migration `0026`): per-org `password_enabled` /
  `social_enabled` toggles for the org's custom-domain login screen.

## Request-time resolution (`custom_domains.go`)

`hostOrgMiddleware` runs **before** `auth.Middleware` (so the org context exists
for the login screen + session handling). It resolves `hostnameOnly(r.Host)` →
an active `org_hostnames` row → stamps an `auth.OrgContext{OrgID, Host}` on the
request. Canonical/unknown/pending hosts → no context, app behaves as always. It's
a single indexed PK lookup; the backend only sees API/ws/share/dav traffic (the
proxy serves static assets), so no cache is warranted.

### Origins (`origin.go` + `custom_domains.go`)

The old trio of duplicated base-URL helpers is unified:

- `canonicalBaseURL()` — the env-backed canonical origin (`TELA_PUBLIC_BASE_URL`),
  used by everything that **stays canonical**: OG/sitemap/JSON-LD, MCP, WebDAV,
  md-export, search.
- `originFor(r)` / `linkOrigin(r)` — the request's effective origin: the custom
  domain when the request arrived on one, else canonical. Used by the **only three**
  browser-facing surfaces that follow a custom domain:
  1. auth verify/reset emails (`verifyLink`/`resetLink`),
  2. the org SSO callback (`ssoCallbackURL`; the org registers
     `https://tela.ngss.io/api/auth/sso/org/callback` at its IdP — authorize and
     token-exchange use the same host so the OIDC redirect_uri byte-matches),
  3. share URLs.
- `shareOrigin(ctx, spaceID)` / `shareOriginForPage(ctx, pageID)` — share URLs are
  derived from the **share's space → org → active hostname** (not the request
  host), so a copied/​unfurled share link is branded with the org's domain wherever
  it was created. Falls back to canonical for personal/no-domain spaces.

Social SSO (Google/Microsoft/GitHub) stays on the canonical callback — those
register one redirect_uri per instance and aren't offered on custom-domain login
screens (the per-org `social_enabled` toggle).

## Session↔org binding

Cookies are already host-only (no `Domain`), so a session minted on `tela.ngss.io`
is never *sent* to another host. The binding hardens that: `CreateSession` stamps
`sessions.org_id` from the request's `OrgContext`; `LoadSessionAndSlide` rejects a
session whose binding doesn't match the request's org context (canonical session on
a custom host, or vice-versa, or wrong org → `ErrInvalidSession`). A forced or
exfiltrated cookie can't be replayed across front doors.

## Login screen + branding (per-org)

`GET /api/host-context` (public, host-derived; the SPA raw-fetches it pre-login)
returns `{ org: { id, name, slug, logo_url, accent }, login: { password_enabled,
social_enabled, org_sso_available } }`. On a custom domain the SPA:

- brands the login screen **and app shell** with the org — name, `logo_url`, and
  `accent` (injected as a runtime `--accent` override that wins over the theme
  stylesheet and survives theme switches; `BrandLogo` is the shared brand
  component across auth header / sidebar / app header);
- shows only the enabled sign-in methods; `password_enabled=false` is **enforced
  server-side** in `Login` (not just hidden), instance admins exempt;
- offers a **one-click org-SSO button** when `org_sso_available` — `SSOStart`
  resolves the org from the request host (no email/domain prompt) when an
  `OrgContext` is present.

Branding (`org_branding`, migration `0027`) and login toggles (`org_login_settings`,
`0026`) are validated (https logo; hex/oklch/rgb accent) and managed by org admins
under **Settings → org → Custom domains** (`OrgManageView`). `GET
/api/orgs/{id}/hostnames/{hostname}/health` is a live DNS + TLS reachability probe
for admin self-diagnosis (SSRF-guarded: never dials a hostname resolving to a
private/loopback address).

## TLS / serving (`deploy/proxy/Caddyfile`, `tls_check.go`)

Direct-TLS mode only (no Cloudflare — CF terminates TLS so on-demand never fires).

- Global `on_demand_tls { ask http://backend:8080/api/internal/tls-check }`. Caddy
  asks before issuing a cert for an unknown SNI host; `TLSCheck` returns 200 iff
  the host is an active `org_hostname`. Without the gate anyone pointing DNS at the
  box could force unbounded issuance. The ask endpoint is on `IsPublicPath`
  (`/api/internal/`) and 404'd from the WAN by both site blocks (Caddy reaches it
  container-to-container).
- A second site block (`https://` catch-all) serves every host the canonical block
  doesn't: app + `/api` + shares only, **blanket `noindex`**. None of the canonical
  block's indexable carve-outs exist here, so a custom-domain app page can't be
  mistaken for the public/SEO surface.

### DNS the org admin sets

1. `_tela-verify.<host> TXT <verify_token>` — proves ownership (the **Verify**
   button resolves it; instance admins can force-activate, skipping DNS).
2. `<host> CNAME <TELA_CUSTOM_DOMAIN_TARGET>` (or `A` → the box IP) — routes
   traffic. `TELA_CUSTOM_DOMAIN_TARGET` is the **one shared** ingress every org
   subdomain points at (not per-org); defaults to the canonical host. The app never
   resolves it — it's only the instruction shown in the UI.

## Config

- `TELA_SITE_ADDRESS` = canonical host (enables direct-TLS; empty → `:80`
  terminator mode, which disables custom domains).
- `TELA_CUSTOM_DOMAIN_TARGET` = shared CNAME target shown to admins (optional;
  defaults to the canonical host).

See `deploy/.env.example`.

## Surfaces that intentionally stay canonical

MCP (`/api/mcp`), WebDAV sync (`/dav/`), OG/sitemap/JSON-LD, RSS, and md-export. These
are agent/machine/SEO surfaces where a single stable identity matters more than
white-labeling; a power user configuring rclone sync will see the canonical host,
which is accepted.

## Tests

`backend/internal/api/custom_domains_test.go`: hostname normalization (apex
rejection), the add→verify→TLS-check→delete lifecycle, validation (apex/duplicate),
the session↔org binding, share-origin derivation, host-context, per-org login
settings, and the server-side host password block.
