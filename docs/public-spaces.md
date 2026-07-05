# Public spaces (blog-style published spaces)

Status: **backend shipped** (migration `0012_public_spaces.sql`). A space-level
"make the whole space public" flag — the foundation for publishing tela content
to the open web. Extends the two-axis model in [`visibility-model.md`](visibility-model.md).

## The model

A space has a `visibility`: `private` (resting — readable only by members,
Axis 1) or `public` (the **whole** space is readable by anyone, no login, at a
clean URL). Public is **whole-space by design** — there are no per-page
exceptions. It is the space-level companion to per-page share links: a share link
is a *capability* ("anyone with the token"), a public space is an *ambient
property* ("anyone, at the page's own address").

### Read-only by construction

`public` is **outbound read exposure only — it never grants write.** The
guarantees, and why they hold without new enforcement code:

- Publishing a space adds **no rows to `space_access`**. Every mutation
  (`POST/PATCH/DELETE /api/pages`, comments, the Yjs collab socket) stays gated
  on membership/role on the session-authed `/api/` routes, so an anonymous
  caller is rejected exactly as before. (`public_spaces_test.go` pins this: anon
  PATCH/POST → 401, page body unchanged.)
- The public read surface is a separate set of **GET-only** handlers under
  `/api/public/` that only ever `SELECT`.
- Flipping `visibility` is **owner-only** (stricter than the editor+ gate on
  name/slug) — publishing a whole space is an owner decision.

## Surface

Migration adds `spaces.visibility TEXT NOT NULL DEFAULT 'private'`
(`CHECK IN ('private','public')`). Set it via `PATCH /api/spaces/{id}`
`{"visibility":"public"}` (owner only).

Public read API — on `auth.IsPublicPath` (`/api/public/`), each handler
self-authenticates by requiring the space be public; a private/missing space is
reported identically as **404** so the endpoint never confirms a private id:

| Route | Returns |
|---|---|
| `GET /api/public/spaces/{id}` | space envelope (id, name, slug, visibility) |
| `GET /api/public/spaces/{id}/tree` | flat page tree (id, title, parent_id, position) |
| `GET /api/public/spaces/{id}/pages/{page_id}` | page: title, body, **props** (frontmatter is public), updated_at |
| `GET /api/public/spaces/{id}/pages/{page_id}/md` | full canonical markdown (`pagemd.Encode`), inline `text/markdown` |

The projection is deliberately narrow — **no comments, history, members, or
cross-space data** leak out. Frontmatter (`props`) **is** public by decision (a
blog publishes its tags/date/summary), so don't stash private metadata there.

`GET /p/{id}` (the public permalink) now redirects a real browser to the no-login
public reader (`/public/spaces/{spaceID}/pages/{id}/{slug}`) when the page's
space is public, instead of the session-gated in-app route; bots still get the OG
envelope.

## Shipped (frontend)

- No-login public reader route `/public/spaces/{id}/pages/{id}/{slug}` (reuses
  the read-only ReaderShell; raw-fetch queries that never bounce to /login).
- Owner-only "Make public / private" toggle in the space dialog; the public
  front-page URL is surfaced when public.
- **Space front page** `/public/spaces/{id}` — a blog-style index: space title +
  top-level posts (title + date) linking to the reader. The reader's topbar
  breadcrumb links back to it.
- **User home page** `/u/{handle}` — a person's front door: their public spaces
  (each → front page) with the spaces' top-level posts (each → reader). Data via
  `GET /api/public/users/{username}` (public spaces by direct membership only;
  404 when the user is missing or has nothing public).
- **Decks present publicly.** A deck (`props.deck`) in a public space is *not*
  rendered as prose — the index card shows its first-slide cover + a "Presentation"
  badge, and the reader shows a cover hero + **Present** (the live Slidev SPA),
  served by visibility-gated public routes (`/api/public/spaces/{id}/pages/{pid}/
  deck/{spa,cover}`). Full design in [`deck.md`](deck.md).

## Deferred / follow-ups

- **Full SEO for public spaces** — DONE. Bot UAs hitting `/public/…` get a
  server-rendered OG document (`public_og.go`): per-page `<title>` + meta
  description (body-derived), OG/Twitter cards, `<link rel="canonical">`,
  JSON-LD (BlogPosting), and — as of the `feat(seo): render public page body`
  change — the **full rendered body** in a crawler-visible `<article>`
  (`renderPublicBodyHTML`, goldmark GFM, raw HTML disabled; custom block
  directives degrade to their inner text, which is what a crawler wants). Both
  sitemaps (`sitemap.xml` marketing, `sitemap-public.xml` enumerating every
  public page) are submitted and `robots.txt` allows `/public/…`. The human
  path stays the client-rendered SPA. Remaining polish: clean/branded indexable
  URLs (ties to "clean readable URLs").
- **`llms.txt`** — an index of a public space's pages (now that there's an
  enumerable public set to point at).
- **Per-page "Published" in a private space** — explicitly skipped. Publicness is
  whole-space only.
- **Caddy** `…/post.md` suffix rewrite → `/api/public/.../md` (the functional
  endpoint exists regardless).
- **OG / link-preview cards** for public-space URLs (currently only share links).
