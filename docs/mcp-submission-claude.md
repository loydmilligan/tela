# tela — Claude connector directory submission

Ready-to-paste payload for `https://clau.de/mcp-directory-submission`.
Values verified live 2026-06-05. `‹TODO›` marks anything Cagdas still has to fill or confirm.

---

## 1. Listing copy

**Server name:** tela

**Tagline (one line):** The team wiki your AI agents can read, search, and write — over MCP.

**Short description (≤ 50 words):**
tela is a self-hostable, markdown-native team wiki with a built-in MCP server. Agents read pages, run full-text and semantic search, and create or edit content alongside the human team. Page bodies are canonical markdown — what an agent writes is exactly what the team sees.

**Long description (2–3 short paragraphs):**

tela is a markdown-native team wiki you self-host. The MCP server is built into the backend, not bolted on as a chat feature: agents authenticate with your account's permissions and get the same read/write surface the web UI uses — list and read spaces and pages, search, create and update pages, comment, and import external pages. Reads and writes are cleanly separated, and write access is scoped to the token.

Knowledge lives as plain markdown (`pages.body` is canonical — there is no proprietary block format), so an agent's edits are diffable, exportable, and identical to what the team reads. Three search paths are exposed: ranked full-text search, body search, and meaning-aware semantic search with citations (page id + heading path), which lets an agent answer a question and cite where the answer came from.

tela is open source (`github.com/zcag/tela`) and first-party — the MCP server domain is the service domain (`tela.cagdas.io`). On hosts that support interactive MCP Apps, results can render as a page-reader widget or search-results cards instead of raw text.

---

## 2. Use cases

Concrete agent workflows, each grounded in the actual tools:

1. **Answer a question from the wiki, with citations.** An agent runs `semantic_search` (or `search`) over the team's pages, reads the top chunks with `read_chunk` / `get_page`, and answers — citing the page id and heading path each claim came from.
2. **Draft a runbook from a deploy log.** Given a deploy or incident log in the conversation, an agent writes a structured runbook and persists it with `create_page` in the relevant space, so the next session (human or agent) starts from durable team memory instead of re-pasting context.
3. **Keep a doc current after a change.** An agent that just shipped a change finds the affected page via `search`, patches the body with `update_page` (which auto-snapshots a revision), and leaves an anchored note with `add_comment`.
4. **Audit what links to a page before editing it.** Before a rename or restructure, an agent calls `list_backlinks` to see every page that references the target, then uses `move_page` / `update_page` without breaking references.
5. **Pull an external doc into the wiki.** An agent imports an external page into a space with `import_mira` (server-side fetch, https-only, allowlisted hosts), turning a one-off link into a durable, searchable page.

---

## 3. Technical

| Field | Value |
|---|---|
| **Endpoint URL** | `https://tela.cagdas.io/api/mcp` |
| **Transport** | Streamable HTTP (current MCP transport; SSE not used) |
| **Auth type** | OAuth 2.1 — WorkOS AuthKit, issuer `https://decisive-relation-32-staging.authkit.app`; PKCE S256 + Dynamic Client Registration. Personal Access Token bearer also accepted. |
| **Read capabilities** | List/read spaces and pages, list backlinks, read page chunks, full-text search, body search, semantic search, document fetch (Deep Research). |
| **Write capabilities** | Create/update/delete pages and spaces, move/reparent pages, add comments, import external pages, submit feedback. Write tools require editor+ scope on the target space; the token's permissions gate every call. |
| **Allowed Link URIs** | `https://tela.cagdas.io/*` (the app surfaces page/space deep links under this origin). `‹TODO: confirm whether the form wants this populated; otherwise leave blank›` |

---

## 4. Data & compliance

- **Third-party connections:**
  - **WorkOS AuthKit** — OAuth 2.1 identity provider for the Connect flow (issuer `decisive-relation-32-staging.authkit.app`). Handles sign-in/consent; tela receives the resulting identity.
  - **External URL fetch (optional, `import_mira`)** — when an agent imports a page, tela fetches the `source_url` server-side. https-only, no redirects followed, restricted to an operator-configured host allowlist (`TELA_MIRA_ALLOWED_HOSTS`). Off the path unless `import_mira` is called.
  - **Remote embedder (optional, `semantic_search`)** — semantic search embeds query/content via an operator-configured Ollama instance (`TELA_RAG_EMBED_URL`); on the public instance this is a dedicated, isolated embed-only endpoint. If unconfigured the tool no-ops (503). No content leaves the operator's infrastructure beyond this embedder.
- **Health data (PHI):** No. tela collects no PHI, PCI, government-ID, or secrets.
- **Data category:** Productivity / knowledge management (team wiki content — markdown documents, comments, space metadata).
- **What is read:** spaces, pages (markdown bodies + metadata), backlinks, comments, and search indexes the authenticated account can access.
- **What is written:** pages (create/update/delete), spaces (create/update/delete), comments, imported pages, and free-text feedback — all under the account's permissions; body changes auto-snapshot a revision.
- **What is stored:** content the agent creates/edits is stored in the operator's PostgreSQL database on the operator's server. tela is self-hosted; on the public instance, data lives on `tela.cagdas.io`'s host. No agent chat history or host memory is read or stored.

---

## 5. Tools, resources, widgets

**Every tool carries a human-readable `title` and read/write annotations** (`readOnlyHint` on reads; `destructiveHint` on deletes; `openWorldHint` on `import_mira`). Names are ≤ 15 chars; read and write are cleanly separated. 20 tools total (10 read / 10 write).

### Read tools (`readOnlyHint: true`)

| Tool | Title | Description |
|---|---|---|
| `list_spaces` | List spaces | List every space the API key can access (id, name, slug). |
| `get_space` | Get space | Fetch a single space's metadata (id, name, slug) by id. |
| `list_pages` | List pages | Flat page listing in a space; optional `parent_id` for direct children. |
| `get_page` | Get page | Full markdown body + metadata for a numeric page id. |
| `list_backlinks` | List backlinks | Pages that link to the given page via `[[wikilink]]` / `tela://page/{id}`. |
| `search` | Search | Ranked full-text search over title + body, snippet-highlighted; optional `space_id`. |
| `search_bodies` | Search page bodies | Ranked full-text body search within one space (no snippets). |
| `semantic_search` | Semantic search | Meaning-aware chunk search (vector + keyword, RRF) with citations (page id + heading path). Requires a configured embedder. |
| `read_chunk` | Read chunk | Fetch one chunk's full section text by `chunk_id` (from `semantic_search`). |
| `fetch` | Fetch document | Fetch a page's full text by id (from a search result). The ChatGPT Deep Research `fetch` tool. |

### Write tools

| Tool | Title | Annotation | Description |
|---|---|---|---|
| `create_space` | Create space | write | Create a space; caller becomes owner. Slug derived from name when omitted. |
| `update_space` | Update space | write | Patch a space's name and/or slug (editor+). |
| `delete_space` | Delete space | `destructiveHint` | Delete a space and all its pages, comments, revisions, share links. Owner only. Irreversible. |
| `create_page` | Create page | write | Create a page in a space (editor+). Body is markdown; `tela://page/{id}` links index as backlinks. |
| `update_page` | Update page | write | Patch a page's title and/or body (editor+). Body change auto-snapshots a revision. |
| `move_page` | Move page | write | Reparent, detach to top-level, reorder, and/or relocate a page to another space (editor+ both sides). |
| `delete_page` | Delete page | `destructiveHint` | Delete a page (editor+). Backlinks preserved with last-known title. |
| `add_comment` | Add comment | write | Attach a root comment anchored by a `{prefix, exact, suffix}` text triplet (editor+). |
| `import_mira` | Import mira page | `openWorldHint` | Import an external mira page into a space (editor+). Server-side https fetch from an allowlisted host, or an inline payload. |
| `submit_feedback` | Submit feedback | write | Submit free-text feedback about tela / tela-mcp itself (not page content). |

### Resources (2 templates)

| URI template | Description |
|---|---|
| `tela://page/{id}` | A page by id. |
| `tela://space/{id}` | A space by id. |

### Interactive widgets (2 — MCP Apps category)

| Widget | Description |
|---|---|
| **page-reader** | Renders a page's markdown as an interactive reader component instead of raw text. |
| **search-results** | Renders search hits as interactive result cards. |

---

## 6. Links

| Field | Value |
|---|---|
| **Documentation** | `https://tela.cagdas.io/mcp` |
| **Privacy policy** | `https://tela.cagdas.io/privacy` |
| **Support channel** | `robot@cagdas.io` |
| **Source** | `https://github.com/zcag/tela` |
| **npm proxy** | `tela-mcp` (`https://www.npmjs.com/package/tela-mcp`) |
| **Review escalation (Anthropic)** | `mcp-review@anthropic.com` |

> Note: `/mcp` and `/privacy` deploy today via `make deploy-landing` — confirm both return 200 before submitting.

---

## 7. Test account — reviewer script

A reviewer connects via OAuth and exercises a read→write round-trip in a demo space.

**Credentials:** `‹TODO: demo login›` (email + password for a populated, no-MFA demo account with at least one space containing several pages, e.g. a "Demo" space with deploy/runbook/incident pages).

**Steps:**

1. **Connect.** Add tela as a custom connector pointing at `https://tela.cagdas.io/api/mcp`. Claude runs the OAuth flow.
2. **Sign in.** The flow opens the tela login screen. Sign in with the demo credentials above (no MFA, no email step). Consent on the WorkOS consent screen.
3. **Tools appear.** After consent the 20 tools become available in chat.
4. **Try, in order:**
   - `list_spaces` → confirm the demo space is listed.
   - `search` for `deploy` → confirm ranked, snippet-highlighted hits.
   - `get_page` on a hit → confirm full markdown body returns.
   - `semantic_search` for a paraphrased question → confirm cited chunks (page id + heading path).
   - `create_page` in the demo space → confirm a new markdown page is created; re-`search` to see it indexed.
   - (optional) `update_page` then `delete_page` on the page just created → confirm the write/destructive round-trip.

Everything runs with the demo account's permissions; an agent can only touch what that account can.

---

## 8. Branding

- **Connector icon:** served by the backend as a full-bleed square SVG **data URI** in the MCP `Implementation.Icons` (`backend/internal/api/mcp.go`), MIME `image/svg+xml`, sizes `any`. Full-bleed by design (no baked-in rounded corners) so the host's rounding mask renders clean — no white corners.
- **Favicon:** `https://tela.cagdas.io/favicon.svg` (source: `landing/public/favicon.svg`).
- **Server branding:** the MCP `Implementation` advertises title **"Tela"** and `WebsiteURL` `https://tela.cagdas.io` for the connector card.
- **Logo asset (standalone, if the form wants a separate upload):** `‹TODO: provide a standalone logo SVG/PNG URL if the data-URI icon isn't accepted as-is›`
- **Widget screenshots (MCP Apps category):** `‹TODO: capture page-reader + search-results carousel screenshots if opting the widgets into the MCP Apps category›`

---

## 9. GA date & tested surfaces

- **GA date:** `‹TODO›`
- **Tested surfaces:** `‹TODO — e.g. Claude.ai web, Claude desktop; note MCP Inspector pass over all 20 tools + 2 resources + 2 widgets›`

---

## Open `‹TODO›`s for Cagdas

- Populated, no-MFA **demo login** (section 7).
- **GA date** + **tested surfaces** (section 9).
- Confirm whether the form wants **Allowed Link URIs** populated (section 3).
- Optional **standalone logo** asset and **widget screenshots** if listing under MCP Apps (section 8).
- Verify `/mcp` and `/privacy` are **live** (200) post-`make deploy-landing` (section 6).
