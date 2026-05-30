# tela — Decisions (ADR-lite)

Each entry: context → decision → consequence.

## D1 — Go backend, single static binary
Want a fast, easy-to-self-host server. Go with the stdlib `net/http` ServeMux (Go 1.22 pattern routing), no web framework. → simple distroless deploy; manual routing means watching the wildcard-then-literal ServeMux limitation.

## D2 — SQLite + FTS5, no separate DB or search engine
A personal/small-team wiki shouldn't require operating Postgres + Elasticsearch. `modernc.org/sqlite` (pure-Go, no cgo) on a Docker volume, with built-in FTS5 for full-text search. → one file, one volume, trivial backup; `:memory:` is per-connection so concurrent tests need an on-disk DB.

## D3 — Hand-written `database/sql`, no ORM / no sqlc
Small schema, full control over queries. → real SQL, no codegen step, no ORM magic; the cost is manual scanning boilerplate.

## D4 — Markdown is canonical; no block model
`pages.body TEXT` is the source of truth forever. The Milkdown editor reads/writes markdown. → portable, diff-able, import/export-friendly; rich features (callouts, collapsibles, diagrams, wikilinks) are markdown extensions + ProseMirror decorations, not a block tree.

## D5 — Yjs as an overlay, custom WS transport
Live collab without making CRDT state the source of truth. Yjs rebases onto canonical markdown on save; a custom 1-byte-tag WebSocket transport instead of y-websocket. → collab is additive and removable; Yjs is deliberately confined to `lib/collab/*` so the rest of the app stays pure-markdown/SQL.

## D6 — Comments anchored by text, not positions
Comments must survive edits and collab reflows. Anchor = `{prefix, exact, suffix}` text triplet resolved against the document text. → robust to position churn; both capture and resolve must use the identical `textBetween` serialization.

## D7 — Owned design system (tokens + Radix/shadcn)
Avoid heavyweight UI kits and ad-hoc styling. Semantic design tokens in `tokens.css`, theming via `[data-theme]`, owned Radix-based primitives only; every new UI element must use an owned primitive (build it with a Storybook story if missing). → consistent, themeable, no MUI/Chakra/etc.; some upfront primitive-building cost.

## D8 — Public share = reuse the frontend in share-mode
Don't build a second renderer. `/share/{token}` mounts the same FE with edit/comments/history hidden; token + HMAC cookie for password gating. → one renderer to maintain; share-mode must hide every authoring surface and use raw `fetch()` (not `api()`, which redirects to /login on 401).

## D9 — MCP as a separate npm package, thin REST client
Agents are first-class, but shouldn't be embedded in the Go binary. `tela-mcp` is a standalone TS/stdio process that bearer-auths and maps each tool to one HTTP call. → independent versioning/publish, clean separation; one extra network hop.

## D10 — Single-tenant first
Self-hosted personal/small-team. Users + spaces + roles, but no orgs/billing; optional per-key space pinning. → simpler; multi-tenancy would be a large future effort.

## D11 — Secrets are HMAC keys, stable across deploys
`TELA_SHARE_SECRET` and `TELA_API_KEY_SECRET` key the share-cookie and PAT HMACs. → rotating either invalidates outstanding share cookies / PATs; they must be set (blank silently produces forgeable tokens) and kept stable.

## D12 — mira import is fetch-hardened
Server-side fetch of attacker-influenced URLs is an SSRF risk. https-only, host allowlist, no redirects, size/time/content-type caps. → safe enough for the threat model; note the allowlist is host-string only (no IP-range guard) — never allowlist a host resolving to a private IP.
