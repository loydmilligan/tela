<!-- Generated 2026-06-06 by a multi-agent SEO/GEO audit (7 parallel surface auditors + research + synthesis). Uncommitted working artifact. -->


# tela — SEO + GEO Audit Report (2026)

## 1. Executive Summary

tela is an unusually SEO-mature property for a v0 dev tool. The marketing landing (Astro static) is near-textbook: perfect Lighthouse (100/100/100/100), LCP 0.4s, CLS 0, zero runtime JS, a well-built JSON-LD entity graph, server-rendered titles/canonicals/OG on every page, and a correct cookie-based noindex strategy that keeps the four landing pages indexable while every app/share/permalink surface is cleanly excluded. The bot-gate for social unfurls is architected end-to-end (Caddy regex in lockstep with the backend UA list), and the HTML-facing copy is factually accurate against the real Postgres + semantic-search architecture. Where it falls short is **discovery and distribution, not hygiene**: the sitemap lists only `/` (the high-value `/mcp` page is invisible to crawlers), the GitHub repo and npm package — a dev tool's strongest off-page authority surfaces — are nearly unoptimized (no description, no topics, no homepage, no keywords, a 404'ing LICENSE), and the two agent-facing files (`llms.txt`/`llms-full.txt`) that exist specifically to be ingested by LLMs are **factually wrong** (SQLite/FTS5, "one binary", 17 tools, a non-existent tool). None of these are deep engineering problems; most are minutes of edits with outsized impact.

**Rough grades by area:**

| Area | Grade |
|---|---|
| Crawlability / Indexation | B+ |
| On-Page (landing + sub-pages) | A− |
| Structured Data | A− |
| Social / Open Graph | B |
| Core Web Vitals / Performance | A |
| Mobile / Accessibility | A− (mobile CWV unverified) |
| SPA Pitfalls | B+ |
| GEO / AEO / LLM files | C+ |
| Off-Page (GitHub / npm / entity) | C− |
| **Overall** | **B+ — strong foundation, under-distributed, with a content-accuracy hole at the LLM surface** |

No §§1,2,4,5,7,10 **[BLOCKER]** is outright failed on the indexable landing pages, so the site clears the "Needs Work" cap — but the off-page blockers (10.1/10.2) and the llms.txt accuracy hole are the gap between "good" and "great."

---

## 2. Top Priorities (ranked, deduplicated, all surfaces)

Ranked by impact × ease. Duplicate findings (e.g. SQLite drift, missing sitemap entries) collapsed to one row with all locations.

| # | Issue | Severity | Surface | Fix | Effort |
|---|---|---|---|---|---|
| 1 | **GitHub repo has no description, no topics, no homepage URL** — zero on-GitHub discoverability, no link equity to site, generic social card | Critical | Off-page | `gh repo edit zcag/tela --description '…' --homepage https://tela.cagdas.io --add-topic wiki --add-topic mcp …` | XS (one command) |
| 2 | **`llms.txt` / `llms-full.txt` assert SQLite/FTS5, "one binary", "no Postgres", 17 tools, `import_markdown`** — verbatim LLM-ingested misinformation, also tells self-hosters they don't need Postgres (breaks deploy) | Critical | GEO/LLM | Rewrite to Postgres + ranked FTS + semantic; drop single-binary; 17→20; remove `import_markdown` | S |
| 3 | **`LICENSE` file missing** but linked from footer, terms, and JSON-LD (3+ live 404s); README says "TBD" | High | Off-page | Commit root MIT `LICENSE`; update README:95; verify the 3 links + JSON-LD resolve 200 | XS |
| 4 | **npm `package.json` missing `keywords`/`repository`/`homepage`/`bugs`** — package page unrankable, leaks no authority back to site/repo | High | Off-page | Add the four fields; publish 0.7.1 | XS |
| 5 | **Sitemap lists only `/`** — `/mcp/` (the top AEO/keyword asset), `/privacy/`, `/terms/` absent from primary discovery channel | High | Crawl + sub-pages + GEO | Add 3 URLs (trailing-slash) with real `lastmod`; better, generate via `@astrojs/sitemap` | S |
| 6 | **EditorMock demo ships REAL `<h1>`/`<h2>`** — live page has 3 `<h1>`s; "Incident response"/"First five minutes" are the first headings a crawler sees | High | Landing on-page | Render mock headings as styled `<div>`/`<p>`, not heading tags | S |
| 7 | **No opt-in to make a public page indexable** — entire UGC/share surface is `noindex`; zero tela content can rank or be AI-cited | High | Share/OG | Ship "Published" opt-in: `indexable` col on `share_links`, owner toggle + privacy warning, emit without noindex + self-canonical + sitemap | L (project) |
| 8 | **Social bot-gate stale** — Bluesky/Mastodon/Slack-ImgProxy/facebookcatalog get blank unfurls on the OSS networks where tela spreads | High | Share/OG | Add `cardyb`, `mastodon`, `slack-imgproxy`, `facebookcatalog` (+ pinterest, redditbot) to `botUASubstrings` AND the Caddy regex in lockstep | S |
| 8b | **Soft-404: SPA returns 200 for unknown paths** (incl. invalid `/share/` tokens to browsers) | High | Crawl/SPA | Emit real 404 for unresolved routes / invalid share tokens | M |
| 9 | **npm description is internal jargon** ("stdio↔HTTP proxy…") — never names the product; unrankable for "mcp"/"wiki"/"knowledge base" | Medium | Off-page | Lead with product + entity keywords, keep proxy nuance second | XS |
| 10 | **Meta description 257 chars** (~100 over SERP truncation) — click payload cut off | Medium | Landing on-page | Trim to ~150–158 chars, front-load hook | XS |
| 11 | **No "Notion/Confluence alternative" / "self-hosted wiki" targeting** in title/H1/any H2; comparison content exists only as `#anchor`, not indexable pages | Medium | Landing on-page | Add phrasing to a heading; split `/compare/notion`, `/compare/confluence` as real pages | M |
| 12 | **`/mcp` lacks TechArticle/BreadcrumbList schema**; shares generic home `og:image` | Medium | Sub-pages | Add page-scoped `TechArticle` (dateModified←`updated` prop) + `BreadcrumbList`; add `/og-mcp.png` | S |
| 13 | **No HTTP→HTTPS 301** (plain `http://` serves 200) | Medium | Crawl/headers | Cloudflare "Always Use HTTPS" | XS |
| 14 | **Unused 193KB React bundle shipped to prod** — dead `@astrojs/react` config, latent footgun | Medium | Performance | Remove react integration from `astro.config.mjs` + deps | XS |
| 15 | **Mobile CWV unverified** — all Lighthouse runs are desktop-only (mobile-first index gates on mobile) | Medium | Performance | Add a mobile Lighthouse pass to the gate | S |
| 16 | **README claims "full-text search"** — search is actually a Postgres ILIKE placeholder; also off-message vs landing's "semantic search" | Medium | Off-page | Change to "semantic + keyword search" / "search" | XS |
| 17 | **`/mcp` invites "open an issue on GitHub"** — contradicts the project's no-issue-tracker rule | Low | Sub-pages | Route to email only; keep GitHub link as source reference | XS |
| 18 | **Internal links use non-slash form** but canonical/sitemap use trailing-slash → 308 hop on every click | Low | Sub-pages | Align hrefs to trailing-slash (or set `trailingSlash:'never'`) | XS |
| 19 | **No `Cache-Control` on landing HTML shell** | Low | Headers/Perf | Add `Cache-Control: no-cache` to apex/non-`_astro` HTML in Caddy | XS |
| 20 | **Sitemap `lastmod` hardcoded** (2026-05-31, stale) | Low | Crawl | Generate at build from file/git mtime | S |
| 21 | **Two render-blocking stylesheets** (SiteFooter + index CSS) keep render-blocking audit at 0.5 | Low | Perf | `build.inlineStylesheets:'auto'` / merge footer CSS | XS |
| 22 | **HSTS missing `includeSubDomains`** (preload deliberately off) | Low | Headers | Optionally add `includeSubDomains` | XS |

---

## 3. By Surface

### 3.1 Marketing Landing — On-Page (`/`)

**Verdict: A−.** Genuinely strong and, importantly, factually accurate vs the live code. One mechanical heading bug and a long meta description are the only real on-page defects; the rest is keyword/linking opportunity.

**What's good:** Unique 54-char keyword-bearing `<title>` in static HTML (`Base.astro:10`); per-subpage titles/descriptions with no default leak; self-referencing canonical matching `og:url`; rich valid JSON-LD `@graph` (SoftwareApplication + SoftwareSourceCode + WebSite + Organization with `sameAs`→GitHub/npm); one *intended* hero H1 with clean H2/H3 outline; copy verified accurate against `search.go`/`search_bodies.go`/`rag.go` (ranked Postgres FTS + hybrid semantic); honest comparison table naming what competitors do better (`Compare.astro:5-11`); complete OG/Twitter in static HTML; skip-link + `<main>` landmark.

**Findings:**
- **[High] EditorMock renders literal markdown as real `<h1>`/`<h2>`** — `EditorMock.astro:62` emits `<h1 data-line="0">Incident response</h1>`, `:68` `<h2>First five minutes</h2>`; embedded twice (`Hero.astro:49`, `AgentWeave.astro:78`). Live `curl … | grep '<h1'` returns **three** H1s; demo data is the first heading a crawler/AI extractor sees. *Fix:* render mock headings as styled `<div>`/`<p>` (or `role="presentation"`), never document headings.
- **[Med] Meta description 257 chars** (`Base.astro:11`) — truncates after "…right inside Claude and ChatGPT"; "Real-time multiplayer, SSO, scoped access" is cut. *Fix:* trim to ~138–158 chars, front-load the hook; keep the long form for `og:description` only.
- **[Med] No high-intent "alternative"/"self-hosted wiki" targeting** — "Notion" appears only as a table row (`Compare.astro:6`); zero "open-source Notion alternative" / "self-hosted Confluence" / "AI team wiki" in title or any H2. *Fix:* put the phrasing in the Compare H2 (or per-competitor H3s); promote rows to indexable `/compare/notion`, `/compare/confluence` pages with own title/H1/canonical/OG.
- **[Low] Sparse internal linking** — only `/mcp` is linked as a real page (`AgentWeave.astro:120`, `Credibility.astro:7`, footer); header/CTA are all `#anchors`. Comparison/security exist only as same-page anchors, not crawlable URLs. *Fix:* real `<a href>` to indexable destinations with varied descriptive anchors.
- **[Low/GEO] Mock prose not `aria-hidden` in Hero** and emits real heading tags regardless — JS-less AI crawlers read demo data as headings. Same fix as the H1 finding.

### 3.2 Crawlability, Indexation & Headers

**Verdict: B+.** Indexation posture is fundamentally sound; the noindex strategy is correctly implemented end-to-end. Discovery (sitemap) and a couple of HTTP-hygiene gaps are the weak spots.

**What's good:** `X-Robots-Tag` policy matches the Caddyfile exactly (landing pages clean, `/login`/`/p/1`/`/api/*`/`/share/*` all noindex); the `@root_app` cookie trick reliably serves the indexable landing to cookieless crawlers; well-formed `robots.txt` (CSS/JS not blocked, AI bots welcomed, correct absolute `Sitemap:`); static canonicals on all four pages; `/p/1` returns a real 404; clean 308 trailing-slash normalization; correct Content-Types; HSTS present; `www` host is effectively dead (TLS fails — no duplicate).

**Findings:**
- **[High] Sitemap lists only `/`** (`landing/public/sitemap.xml:3-8`) — `/mcp/`, `/privacy/`, `/terms/` all 200/indexable but absent. *Fix:* add all three (trailing-slash) with real `lastmod`; generate via `@astrojs/sitemap`.
- **[High] Soft-404** — `/this-does-not-exist-xyz` and `/foo/bar` → HTTP 200 (noindex shell); invalid `/share/` token → 200 to browsers but 404 to Twitterbot (inconsistent). Caddyfile catch-all (`deploy/proxy/Caddyfile:117-133`) sends all non-landing paths to the SPA. *Fix:* emit real 404 for unresolved routes and invalid share tokens.
- **[Med] No HTTP→HTTPS 301** — `http://tela.cagdas.io/` serves 200 (`Caddyfile:11`, no CF redirect). *Fix:* Cloudflare "Always Use HTTPS."
- **[Low] HSTS** lacks `includeSubDomains` (`Caddyfile:14`); preload deliberately off — keep it off.
- **[Low] Stale hardcoded `lastmod`** 2026-05-31 (`sitemap.xml:5`) while sources edited Jun 5–6.
- **[Low] No `Cache-Control` on landing HTML shell** — only `/_astro/*` cached; apex is CF `DYNAMIC`.

### 3.3 Performance / Core Web Vitals (Landing)

**Verdict: A.** Effectively maxed on the measured profile. The only real gaps are an unused bundle and the absence of a mobile measurement artifact.

**What's good:** Latest Lighthouse 1.0/1.0/1.0/1.0 (`lhr-1780675707682.json`); LCP 0.4s / TBT 0ms / CLS 0; **zero runtime JS, zero `<img>`** (9 requests, ~121KB, 89KB fonts); textbook font strategy (36 inlined `@font-face`, `font-display:swap`, 18 metric-matched Arial fallbacks → CLS genuinely 0, 2 woff2 preloads); Brotli end-to-end (CSS 63KB→9.7KB); fingerprinted assets `immutable`; small DOM (861 els); gate `passed`.

**Findings:**
- **[Med] Unused 193KB React bundle in prod** — `dist/_astro/client.lFkSSlRS.js` (193,540 B, React runtime) referenced by no page; `astro.config.mjs:5,11` registers `react()` with zero `.tsx`/`.jsx` in `src/`. Live-reachable (200). Latent footgun the moment an island is added. *Fix:* remove `@astrojs/react` integration + deps; rebuild and confirm no `client.*.js`.
- **[Med] Mobile CWV unverified** — `lighthouserc.cjs:43` is `preset:'desktop'` (no throttling, 1350×940); no mobile artifact exists. Search judges CWV on the mobile render. *Fix:* add a mobile Lighthouse pass with the strict LCP/CLS/TBT assertions.
- **[Low] No `Cache-Control` on HTML shell** (mirrors crawl finding) — app shell already standardized on no-cache (commits a46f555/fb8fe24); landing apex was missed.
- **[Low] Two render-blocking stylesheets** (`SiteFooter.MEJ9oDjb.css` + `index.BstbX6nj.css`) keep render-blocking audit at 0.5. *Fix:* `build.inlineStylesheets:'auto'` / merge footer CSS.
- **[Info] Only 2 of 4 fonts preloaded** — correct by design; do not preload all four.

### 3.4 Sub-Pages — `/mcp`, `/privacy`, `/terms`

**Verdict: A−.** All three are clean, statically rendered, and pass the on-page basics. `/mcp` is a genuinely substantive, accurate content asset (~1,100 words, correct 20-tool roster). The defects are discovery (sitemap) and missing page-level schema/OG.

**What's good:** Unique server-rendered titles/descriptions (no default leak); self-referencing trailing-slash canonicals matching `og:url`; exactly one H1 each (`Prose.astro`); `/mcp` is deep (endpoint/auth/troubleshooting, Streamable HTTP, OAuth 2.1 + PKCE via WorkOS) and **factually accurate** — "Twenty tools, 10 read / 10 write" matches `mcp_tools.go`, and it correctly avoids the SQLite/FTS5 drift; all three reachable via real `<a href>` (no orphans); privacy/terms copy accurate to the real architecture.

**Findings:**
- **[High] `/mcp`, `/privacy`, `/terms` missing from sitemap** — `/mcp` is the single best AEO/keyword asset and has zero sitemap presence. *(Same root as 3.2.)*
- **[Med] `/mcp` lacks TechArticle/BreadcrumbList** — only the inherited site-wide graph; visible "June 5, 2026 updated" isn't machine-readable. *Fix:* page-scoped `TechArticle` (headline, `dateModified`←`updated` prop, author/publisher→`#org`) + `BreadcrumbList` (`mcp.astro:32-38`).
- **[Low] All sub-pages share the generic home `og-image.png`** — `/mcp` (a shared-to-recommend doc) unfurls identically to legal pages. *Fix:* add `/og-mcp.png` (1200×630, title baked in); thread `ogImage` through `Prose.astro`→`Base.astro`. Privacy/terms sharing it is fine.
- **[Low] Canonical/sitemap use trailing-slash, internal links don't** (`SiteFooter.astro:22`, `AgentWeave.astro:120`, `mcp.astro:135`, `terms.astro:52`) → 308 hop per click. *Fix:* align hrefs or set `trailingSlash:'never'`.
- **[Low] `/mcp` Troubleshooting says "open an issue on GitHub"** (`mcp.astro:159`) — contradicts the project's no-issue-tracker rule (and the same page's security section). *Fix:* route to email only.

### 3.5 Share Pages, Permalinks & Social OG

**Verdict: B.** The bot-gate architecture is sound and defense-in-depth; the gated OG envelope is accurate. Two high-impact gaps: a stale UA list blanks several networks, and there is no way to make any public page indexable — so the whole UGC surface is invisible to Search/AI.

**What's good:** Handlers re-check `isBotUA()` so a Caddy misconfig fails closed to 404 (`public_share_link.go:44-47`); Caddy `share_bots` regex and backend `botUASubstrings` in sync (`Caddyfile:75` vs `public_share.go:18-28`); OG image strong (1200×630 PNG, weak ETag, 304 path, `og_image.go:113-135`); locked shares emit a generic envelope with no `og:image` (never leak protected pages); missing/revoked/expired all collapse to one 404; OG fields HTML-escaped.

**Findings:**
- **[High] Bluesky/Mastodon/Slack-ImgProxy/facebookcatalog blank unfurls** — `Bluesky Cardyb/1.1`, `Mastodon/4.2`, `Slack-ImgProxy`, `facebookcatalog` hit the SPA shell (200), not the OG handler; absent from `botUASubstrings` (`public_share.go:18-28`). *Fix:* add `cardyb`, `mastodon`, `slack-imgproxy`, `facebookcatalog` (+ `pinterest`, `redditbot`) to **both** the backend list and the Caddy regex in lockstep.
- **[High] No opt-in to make a public page indexable** — Caddyfile noindexes all share (`78,82`) and permalink (`28`) paths; `share_links` has no `indexable` column; "Published" state is Deferred (`docs/visibility-model.md:66-67,111-116`). The wiki's best growth/AI-citation asset is fully off. *Fix:* ship "Published" as owner opt-in — `indexable` boolean, Share-manager privacy warning, emit without noindex + self-canonical + meta-description + sitemap entry; default stays noindex.
- *(Also noted by other auditors: invalid share tokens 200 soft-404 to browsers; envelope omits image dims/`site_name`/twitter tags; Googlebot gets the empty-body stub.)*

### 3.6 GEO / AEO / LLM Files

**Verdict: C+.** The HTML-facing GEO surfaces are excellent and modern; the agent-facing `.txt` files — which exist to be ingested verbatim — are the single biggest content-accuracy liability in the audit.

**What's good:** Well-built JSON-LD `@graph` (correct shape for entity understanding); accurate site-wide meta ("search your docs by meaning… inside Claude and ChatGPT", `Base.astro:11,75`); `Faq.astro` accurate and answer-first in native `<details>`, correctly omitting retired `FAQPage` schema (documented choice, `Faq.astro:3`); `robots.txt` deliberately welcomes AI/retrieval crawlers; `llms-full.txt` is well-*structured* (H1 + blockquote + scoped `##` + copy-paste `.mcp.json`) — the right llmstxt.org shape, only the *content* is wrong.

**Findings:** (see §4 for exact corrected wording)
- **[Critical] SQLite/FTS5 storage + search claims** — `llms.txt:3,5,12`; `llms-full.txt:10,15,51`. Reality: PostgreSQL via pgx, ranked Postgres FTS (`ts_rank_cd` over `pages.search_tsv`, migrations 0003/0004). Tells self-hosters "no Postgres" — which breaks their deploy.
- **[High] "One binary / single Go binary"** — `llms.txt:3`; `llms-full.txt:3,10,15,42,45,47,51`. Postgres is a separate required container; also violates the project rule against marketing single-binary.
- **[High] Tool count wrong (17→20) + non-existent `import_markdown`** — `llms.txt:5`; `llms-full.txt:7,12,50`. Real tool is `import_mira`; 20 tools registered in `mcp_tools.go`.
- **[Med] "search (FTS5)" / "real full-text search" undersell semantic search** — `llms-full.txt:12,47`. `semantic_search`/`read_chunk` ship (vector+keyword RRF with citations) — the strongest GEO hook, buried.
- **[Low] llms.txt not discoverable beyond URL-guessing** — contested by design (no provider commits to reading it); fix the facts, don't chase discoverability.
- **[Low] Organization `sameAs` omits X/LinkedIn/docs; no `softwareVersion`/`dateModified`** — add only identities that genuinely exist; `aggregateRating` correctly absent.

### 3.7 Off-Page / Distribution (GitHub, npm, entity)

**Verdict: C−.** The highest-leverage, lowest-effort wins in the entire audit live here, and they're nearly all unaddressed. The landing's *outbound* linking to GitHub/npm is the bright spot; the destinations themselves are orphaned.

**What's good:** Landing links out to GitHub/npm in multiple visible places with `sameAs` mirroring real `<a href>` (entity consistency); JSON-LD entity grounding strong; npm published under a clean name (`tela-mcp`), MIT, signed, active cadence (0.1.0→0.7.0 in ~2 weeks); `mcp/README.md` is high-quality and honest ("most hosts don't need this package").

**Findings:**
- **[Critical] GitHub repo: no description, no topics, no homepage URL** — `gh repo view zcag/tela` → all empty. The single highest-leverage off-page fix; one `gh repo edit` command (see #1).
- **[High] npm `package.json` missing `keywords`/`repository`/`homepage`/`bugs`** (`mcp/package.json:5-13`) — page unrankable, no repo/provenance badge, no backlink to site. *Fix:* add fields, publish 0.7.1.
- **[High] LICENSE 404** — no root `LICENSE`; linked from `SiteFooter.astro:27`, `terms.astro:25`, and JSON-LD `Base.astro:81`; README:95 "TBD". *Fix:* commit root MIT `LICENSE`, verify all four resolve 200.
- **[Med] npm description is internal jargon** (`mcp/package.json:4`) — leads with "stdio↔HTTP proxy", never names the product. *Fix:* product + entity keywords first.
- **[Med] GitHub generic auto social-card** — "Contribute to zcag/tela…" with no product name. *Fix:* upload 1280×640 social-preview (adapt `og-image.png`); the About description auto-fixes `og:description`.
- **[Med] README claims "full-text search"** (`README.md:8`) — search is an unranked ILIKE placeholder (`TODO(search)`); off-message vs landing's "semantic search." *Fix:* "semantic + keyword search" / "search."
- **[Low] README license "TBD"** (`README.md:95`) — pairs with missing LICENSE; resolve and badge.

---

## 4. Content-Accuracy Hitlist

Every factually wrong/stale **public** claim, with exact location and corrected wording. The `.txt` files are highest-priority because they are ingested verbatim by LLMs.

| File:line | Wrong claim | Correct wording |
|---|---|---|
| `landing/public/llms.txt:3` | "instant SQLite full-text search, one binary" | "Live multiplayer, ranked full-text + semantic search, your data on your server." |
| `landing/public/llms.txt:5` | "Go + SQLite/FTS5 backend"; "17 scoped tools" | "Go + PostgreSQL backend, React/Milkdown editor, Yjs live collaboration"; "20 scoped tools" |
| `landing/public/llms.txt:12` | "Go backend (SQLite + FTS5)" | "Go backend (PostgreSQL), React/Milkdown frontend, and the MCP server." |
| `landing/public/llms-full.txt:3` | "…one binary." | "…Self-host with docker compose. Your data, your server." |
| `landing/public/llms-full.txt:7,50` | "17 tools" | "20 tools" (or "~20 scoped tools" to avoid drift) |
| `landing/public/llms-full.txt:10` | "storage and full-text search are SQLite + FTS5 — no Postgres"; "single binary" | "Go backend + PostgreSQL; full-text search is Postgres tsvector (ts_rank_cd), with optional semantic search via an embedder. docker compose up." |
| `landing/public/llms-full.txt:12` | "search (FTS5)"; "import_markdown (bulk)" | "search (ranked Postgres full-text) and semantic_search (vector+keyword RRF, cited chunks)"; replace `import_markdown` with `import_mira`, add `semantic_search`/`read_chunk`/`fetch`/`move_page`/`submit_feedback` |
| `landing/public/llms-full.txt:15` | "Storage + full-text search: SQLite + FTS5"; "compiled to a single binary" | "Backend: Go + PostgreSQL. Search: ranked Postgres full-text (tsvector) + optional semantic/RAG." |
| `landing/public/llms-full.txt:42,45,47` | "one binary you run yourself" / "in a single binary you run yourself" | "you run it yourself with docker compose, no per-seat pricing, your data on your disk" |
| `landing/public/llms-full.txt:47` | "real full-text search" | "ranked full-text plus semantic search with citations" |
| `landing/public/llms-full.txt:51` | "Do I need Postgres? No."; "inside the single Go binary" | "Do I need Postgres? Yes — tela runs on PostgreSQL (via docker compose). Full-text search is built in (Postgres tsvector); semantic search additionally needs an embedding model. No Elasticsearch." |
| `README.md:8` | "full-text search" | "semantic + keyword search" (or "search") |
| `README.md:95` | "License: TBD" | definitive license (MIT) + badge, after committing root `LICENSE` |
| `mcp/package.json:4` | "Thin stdio↔HTTP proxy to a Tela instance's…" | "MCP server for Tela — a self-hostable markdown team wiki. Lets Claude, Cursor and other agents search, read and write your wiki pages. Stdio↔HTTP proxy to {TELA_BASE_URL}/api/mcp for hosts that can't speak HTTP directly." |
| `landing/src/pages/mcp.astro:159` | "open an issue on GitHub" | "Email tela@cagdas.io — the source is on GitHub." (no issue-filing CTA) |
| `Base.astro:81` / `SiteFooter.astro:27` / `terms.astro:25` | LICENSE URL → 404 | resolves 200 after committing root `LICENSE` |

> **Note (no change needed):** `mcp.astro:13,140` "Full-text search" and the home/sub-page HTML copy are **accurate** against `search.go`/`search_bodies.go` (ranked Postgres FTS). The HTML surfaces are correct — the drift is confined to the `.txt` files, the README, and npm metadata.

---

## 5. Quick Wins vs. Projects

### Quick wins (minutes to ~1 hour; outsized impact)
- **GitHub About:** one `gh repo edit zcag/tela --description '…' --homepage https://tela.cagdas.io --add-topic wiki --add-topic mcp --add-topic self-hosted …` — #1 highest-leverage fix.
- **Commit a root MIT `LICENSE`** → kills 3 live 404s + JSON-LD broken link, unlocks GitHub license badge.
- **Add 4 npm fields** (`keywords`/`repository`/`homepage`/`bugs`) + rewrite description → publish 0.7.1.
- **Rewrite `llms.txt`/`llms-full.txt`** to current architecture (SQLite→Postgres, drop single-binary, 17→20, fix `import_markdown`) — pure content edits.
- **Add `/mcp/`, `/privacy/`, `/terms/` to `sitemap.xml`** (trailing-slash, real `lastmod`).
- **Trim meta description** to ~150 chars (`Base.astro:11`).
- **Demote EditorMock headings** to `<div>`/`<p>` (`EditorMock.astro:62,68`) → restores single H1.
- **Add bot-gate UAs** `cardyb`/`mastodon`/`slack-imgproxy`/`facebookcatalog` to both lists.
- **Cloudflare "Always Use HTTPS"** (one toggle).
- **Remove dead `@astrojs/react`** from `astro.config.mjs` → drop 193KB from the build.
- **Fix README "full-text search" → "semantic + keyword search"**; `/mcp` "open an issue" → email.
- **Align internal hrefs to trailing-slash** (kill the 308 hop).
- **Add `Cache-Control: no-cache`** to landing HTML in Caddy.

### Projects (design/dev work)
- **Indexable "Published" share state** — `indexable` column on `share_links`, owner opt-in with privacy warning, emit without noindex + self-canonical + meta + sitemap entry (default noindex). Unlocks the entire UGC growth/AEO surface. *(docs/visibility-model.md has it as Deferred.)*
- **Real 404 status for unresolved SPA routes & invalid share tokens** — stop the soft-404 200s.
- **Standalone comparison pages** (`/compare/notion`, `/compare/confluence`) with own title/H1/canonical/OG, linked from the table — captures the highest-intent dev queries.
- **Build-time sitemap generation** (`@astrojs/sitemap`) with per-page `lastmod` — so it never drifts again.
- **Page-scoped schema** — `TechArticle` + `BreadcrumbList` on `/mcp` (and a thread for `ogImage`/`/og-mcp.png`).
- **Mobile Lighthouse pass in the gate** — produce a real mobile-first CWV artifact.
- **Custom GitHub social-preview image** (1280×640).

---

# Appendix A — Grading Rubric (2026 SEO/GEO, primary-source-grounded)


# 2026 SEO / GEO Audit Rubric — Open-Source Dev Tool (landing + SPA app + public shareable pages + MCP/npm)

**How to score:** Grade each item **Pass / Partial / Fail / N/A**. Items tagged **[BLOCKER]** can sink the whole bucket if failed. **[CONTESTED]** = the industry disagrees or Google has explicitly downplayed it — grade it, but weight it low and note the dispute. Verification tools are named per item; default toolbelt: **Google Search Console (GSC)** URL Inspection + Coverage/Pages report, **Rich Results Test**, **PageSpeed Insights (PSI)** + **CrUX**, `curl`/`wget` (raw vs rendered HTML), Chrome DevTools (Lighthouse, Coverage, Performance), **Schema.org Validator**, social debuggers (Facebook Sharing Debugger, X Card validator, LinkedIn Post Inspector), and a headless fetch to simulate a JS-less crawler.

**North-star context (2026):** Google's own May 15 2026 AI Optimization Guide states generative features (AI Overviews, AI Mode) run on the **same index, same ranking, same E-E-A-T** as classic Search — "AEO/GEO is still SEO." So classic-SEO hygiene is the dominant lever; treat GEO-specific tactics as additive, not substitutive.

---

## 1. Crawlability & Indexation

| # | Item | Why it matters | How to verify |
|---|------|----------------|---------------|
| 1.1 **[BLOCKER]** | Site is **crawlable, indexable, renderable, understandable** (the four Search Essentials pillars) | Minimum bar for any Google appearance — including AI Overviews/AI Mode, which require standard indexing + snippet eligibility | GSC URL Inspection → "URL is on Google" + "Crawled/Indexed"; Coverage report shows no mass exclusions |
| 1.2 **[BLOCKER]** | `robots.txt` present at root, does **not** block CSS/JS or important paths; no accidental `Disallow: /` | A blocked JS/CSS bundle breaks rendering → SPA content invisible | Fetch `/robots.txt`; GSC URL Inspection "Test Live URL" → check rendered screenshot & blocked resources |
| 1.3 | **XML sitemap** present, referenced in `robots.txt` (`Sitemap:` line) and submitted in GSC; lists canonical landing pages + indexable public doc pages; excludes app/auth routes; `<lastmod>` accurate | Primary discovery channel for new/updated URLs and orphan-prone SPA routes | Fetch sitemap; GSC Sitemaps report (status + discovered count); spot-check `lastmod` reflects real edits |
| 1.4 | **Logical internal linking**, no orphan pages; public doc pages reachable via real `<a href>` from indexable pages | Googlebot discovers via links; orphaned share pages never get crawled | Crawl with a JS-less fetcher; confirm doc/landing URLs appear as `<a href>` in static HTML |
| 1.5 | Correct **HTTP status semantics**: 200 only for real content, true **404/410** for missing, 301 for moved; no **soft-404s** (200 + "not found" body) | SPAs notoriously return 200 for everything → wastes crawl budget, pollutes index | `curl -I` a deleted/invalid doc URL → must be 4xx, not 200; GSC Coverage "Soft 404" bucket empty |
| 1.6 | **Canonical host + scheme** enforced (one of www/non-www, HTTPS only, 301 the rest); no mixed duplicate homepages | Duplicate hosts split signals and waste crawl budget | `curl -I http://`, `http://www`, trailing-slash variants → all 301 to one canonical |
| 1.7 | **Crawl budget not wasted** on infinite/parameterized SPA URLs, faceted junk, or session params | Dev-tool apps generate many low-value client routes | GSC Crawl Stats; server logs for Googlebot hitting app-internal URLs that should be noindex/disallowed |
| 1.8 | **No `noindex` leakage** onto pages you want indexed (common SPA footgun: global noindex meta injected before route resolves) | A stray `noindex` silently de-indexes | URL Inspection → "Indexing allowed? Yes"; check rendered (not raw) `<meta robots>` |
| 1.9 | Mobile and desktop serve the **same content + same robots meta** (mobile-first indexing, default since Oct 2023) | Google indexes the mobile rendering; divergent noindex/nofollow on mobile = dropped pages | Compare rendered mobile vs desktop HTML; URL Inspection mobile usability |

---

## 2. On-Page (titles / meta / headings / canonical / linking / content)

| # | Item | Why it matters | How to verify |
|---|------|----------------|---------------|
| 2.1 **[BLOCKER]** | **Unique, descriptive `<title>`** per route, present in the **server/static HTML** (not only injected post-hydration) for landing + each public doc page | Title is a top on-page signal and the SERP/unfurl headline; client-only titles risk being missed | View-source (not DevTools) the raw HTML; confirm `<title>` populated pre-JS |
| 2.2 | **Meta description** unique per page, ~110–160 chars, compelling (influences CTR; Google may rewrite) | CTR + snippet quality | View-source; check uniqueness across pages |
| 2.3 **[BLOCKER]** | **One `<h1>`** per page, logical `h2/h3` outline mirroring content; semantic HTML (`<main>`, `<article>`, `<nav>`) | Heading hierarchy = primary content-structure signal for both Search and AI extraction | DevTools accessibility tree / headingsMap; confirm single h1 |
| 2.4 **[BLOCKER]** | **Self-referencing `rel=canonical`** on every indexable page; public doc pages canonical to their stable public URL (not the app/edit URL); absolute HTTPS URL | Prevents duplicate-content dilution; share pages especially prone to multiple URLs | View-source canonical; confirm it matches the URL and isn't a stale/templated default |
| 2.5 | **Descriptive, stable, lowercase URLs** with words (not opaque IDs only where avoidable); History API routing, **no `#`/hashbang** routes | Hash routes aren't crawled as separate pages; clean URLs aid ranking + shareability | Inspect router config; confirm doc URLs are path-based (`/p/slug`) not `/#/...` |
| 2.6 | **Internal linking with descriptive anchor text**; landing → docs → app cross-links; related-page links on doc pages | Distributes authority, aids discovery, gives context | Crawl; review anchor text (avoid "click here") |
| 2.7 | **Content depth & originality**: landing communicates value clearly; docs/use-case pages provide first-hand, non-commodity content (not AI-rehashed boilerplate) | 2026 Google guide: "don't recycle what others said / what an LLM could produce" — originality drives both ranking and AI citation | Manual read; check for unique data, examples, opinions vs generic filler |
| 2.8 | **Keyword/intent targeting**: pages map to real query intent (e.g. "open-source [category] tool", "[tool] vs [competitor]", "[tool] MCP server"); entities named explicitly | Match query intent; comparison/alternative pages are high-intent for dev tools | Keyword research vs page inventory; confirm a page exists per priority intent |
| 2.9 | **Image SEO**: descriptive `alt`, meaningful filenames, modern formats (WebP/AVIF), explicit `width/height` | Alt = a11y + image search + CLS prevention | DevTools; Lighthouse a11y audit |
| 2.10 | **No keyword stuffing / cloaking / doorway pages**; content shown to users == content shown to bots | Spam-policy violations = manual action risk | Compare rendered HTML for Googlebot UA vs normal UA |

---

## 3. Structured Data (schema.org / JSON-LD)

| # | Item | Why it matters | How to verify |
|---|------|----------------|---------------|
| 3.1 | **JSON-LD** (not microdata), valid, in `<head>` or body, ideally in static/SSR HTML or reliably injected before render | JSON-LD is Google's preferred format; client-injected SD must survive rendering | Rich Results Test (renders JS) + Schema.org Validator |
| 3.2 | **`SoftwareApplication`** (or `SoftwareSourceCode`) on landing: `name`, `applicationCategory: DeveloperApplication`, `operatingSystem`, `offers` (free/$0), `softwareVersion`, `aggregateRating` only if genuinely earned | Describes the product as a software entity → eligibility + entity understanding for AI | Rich Results Test; confirm no fake ratings (review-snippet spam policy) |
| 3.3 | **`Organization`** schema (site-wide): `name`, `url`, `logo`, `sameAs` → GitHub, npm, X, LinkedIn, docs | Builds the brand entity in Google's Knowledge Graph; `sameAs` links the dev-tool's social/repo identities | Validator; confirm `sameAs` array present and correct |
| 3.4 | **`BreadcrumbList`** on docs/doc-share pages | Still a live rich result; aids hierarchy understanding | Rich Results Test → Breadcrumb eligible |
| 3.5 | **`Article`/`TechArticle`** (or `WebPage`) on doc/blog pages with `headline`, `datePublished`, `dateModified`, `author` (a real person/org with credentials) | `author` + dates feed E-E-A-T and freshness signals | Validator; confirm `dateModified` updates on real edits |
| 3.6 **[CONTESTED]** | **`FAQPage`** schema where genuinely Q&A — but know it **no longer produces rich results** (FAQ rich results dropped May 7 2026; report/Rich-Results-Test support removed June 2026; surfaces only for authoritative gov/health). HowTo rich results gone since 2023. | No SERP rich result anymore, BUT widely reported that FAQ-structured Q&A still aids AI Overviews / ChatGPT / Perplexity extraction. Keep markup if content is truly FAQ; don't add it just for a rich result that no longer exists. Don't bother removing existing valid FAQ markup. | Confirm markup matches visible content; do NOT expect a SERP FAQ panel; note this is contested-value |
| 3.7 | **No invalid/spammy SD**: every `review`/`rating` corresponds to real on-page reviews; no markup for hidden content | Structured-data spam → manual action; unused-but-valid SD is harmless but earns nothing | Rich Results Test "valid items"; manual policy check |

---

## 4. Social / Open Graph (critical for the shareable doc-page unfurls)

| # | Item | Why it matters | How to verify |
|---|------|----------------|---------------|
| 4.1 **[BLOCKER]** | Core **OG tags** on landing + **every public doc page**: `og:title`, `og:description`, `og:url` (absolute, canonical), `og:type`, `og:site_name`, `og:image` | Drives the unfurl card in Slack/Discord/X/LinkedIn — a primary distribution channel for shareable docs | Facebook Sharing Debugger + LinkedIn Post Inspector on a live doc URL |
| 4.2 **[BLOCKER]** | **`og:image`** is an absolute HTTPS URL, **1200×630** (1.91:1), <1MB (PNG/JPG/WebP), with key content in the centered safe zone; **per-page dynamic OG image** for doc pages (title baked in) is ideal | A generic/missing image kills click-through on shared docs; per-page images make each shared doc distinct | Open the og:image URL directly; check dimensions; share a doc and view the rendered card |
| 4.3 | **Twitter/X cards**: `twitter:card=summary_large_image`, `twitter:title`, `twitter:description`, `twitter:image` (can reuse og:image) | X falls back inconsistently without explicit tags | X Card validator (or post + preview) |
| 4.4 **[BLOCKER for SPA]** | OG/Twitter tags are present in the **server-rendered/prerendered HTML the scraper receives** — social scrapers (Slack, X, Discord, Facebook) **do NOT execute JS** | This is the #1 SPA unfurl failure: client-injected OG tags → blank cards. Public doc pages likely need SSR/prerender or an edge meta-injection just for crawlers/scrapers | `curl -A "facebookexternalhit" <docurl>` → OG tags must be in raw response |
| 4.5 | `og:image:alt`, `og:locale`, and image `:width`/`:height` set | Accessibility + faster, correct first-render of cards | View-source |

---

## 5. Core Web Vitals & Performance

| # | Item | Why it matters | How to verify |
|---|------|----------------|---------------|
| 5.1 **[BLOCKER]** | **INP ≤ 200 ms** (Good); 200–500 = needs work; >500 = Poor — at **75th percentile of real users** | INP is the responsiveness CWV (replaced FID Mar 2024); 2026's most-failed vital (~43% of sites fail). SPA/editor apps with heavy JS are most at risk | PSI/CrUX field data (not lab); `web-vitals` JS `onINP()` in RUM |
| 5.2 **[BLOCKER]** | **LCP ≤ 2.5 s** (Good; ≤4 s needs work) at p75 | Loading perception; CWV ranking signal | PSI field data; Lighthouse lab as proxy |
| 5.3 **[BLOCKER]** | **CLS ≤ 0.1** (Good; ≤0.25 needs work) at p75 | Visual stability; reserve space for images/fonts/embeds | PSI; DevTools Performance layout-shift regions |
| 5.4 | **CWV pass = ≥75% of visits "Good"** across all three; tracked separately for mobile and desktop, landing vs app | "Pass" is a percentile bar, not an average; the editor/app may pass while landing fails or vice-versa | GSC Core Web Vitals report (URL groups); CrUX per-URL |
| 5.5 | **JS hygiene for INP**: code-split, break long tasks (>50ms), defer non-critical JS, avoid hydration jank; landing ships minimal JS (static), app lazy-loads routes | Long main-thread tasks are the INP killer; landing should be near-static | DevTools Coverage (unused JS) + Performance long-tasks; Lighthouse TBT as lab proxy |
| 5.6 | **LCP optimization**: preload LCP image/font, `fetchpriority=high`, no render-blocking resources, CDN, compression (Brotli), HTTP/2-3 | Faster LCP + better crawl | PSI opportunities; WebPageTest |
| 5.7 | **Caching/immutable assets** correct (fingerprinted assets `immutable`, HTML `no-cache`) — repo already moved on this | Repeat-view speed; avoids stale-shell bugs | `curl -I` an asset and the HTML shell; check `Cache-Control` |

---

## 6. Mobile & Accessibility (as ranking + AI signals)

| # | Item | Why it matters | How to verify |
|---|------|----------------|---------------|
| 6.1 **[BLOCKER]** | **Mobile-responsive**, content parity with desktop, tap targets ≥48px, no horizontal scroll, `<meta name="viewport">` set | Mobile-first indexing means the mobile render IS what's indexed | PSI mobile; DevTools device emulation |
| 6.2 | **Accessibility**: semantic landmarks, alt text, label/forms, color contrast (WCAG AA), keyboard nav, focus states | A11y overlaps heavily with crawl/parse quality and AI legibility; Google treats good page experience holistically | Lighthouse a11y score; axe DevTools |
| 6.3 | **Readable structure**: real paragraphs, lists, tables, headings (not div-soup) | Google's AI guide explicitly cites clear paragraphs/sections/headings as what helps content surface in AI features | Manual + heading map |
| 6.4 | **No intrusive interstitials** on mobile (cookie/login walls blocking content) | Intrusive-interstitial penalty; also blocks AI/crawler access to doc content | Load a public doc on mobile; confirm content visible without modal wall |

---

## 7. SPA-Specific Indexing Pitfalls

| # | Item | Why it matters | How to verify |
|---|------|----------------|---------------|
| 7.1 **[BLOCKER]** | **Critical content present in rendered HTML** within Googlebot's render budget; prefer **SSR/SSG/prerender** for landing + public doc pages over pure client render | Googlebot renders JS but with delay/budget; non-Google AI crawlers and social scrapers often **don't run JS at all** → empty pages | `curl` raw HTML for content; GSC URL Inspection "View rendered HTML" + screenshot |
| 7.2 **[BLOCKER]** | **History API routing** with crawlable `<a href>` links and unique URLs per view; **no hashbang** | Hash fragments aren't indexed as distinct pages | Inspect links; confirm each doc has a unique non-hash URL returning content |
| 7.3 **[BLOCKER]** | **Per-route `<title>`, meta description, canonical, OG** correctly set and present for crawlers (server-injected or prerendered for bot UAs) | The classic SPA failure: all routes share the index.html's default head | Fetch several doc URLs raw; confirm head differs per page |
| 7.4 | **No soft-404 for missing docs** — SPA returns real 404 status + noindex for unknown slugs | Else Google indexes infinite empty app shells | `curl -I /p/does-not-exist` → 404 |
| 7.5 | **App routes that should NOT be indexed** (editor, settings, auth, dashboard) are `noindex` and/or disallowed; only public marketing + share pages are indexable | Keeps the index clean, protects private/app surfaces, focuses crawl budget | URL Inspection on an app route → "Excluded by noindex"; check robots/meta |
| 7.6 | **Lazy-loaded / infinite-scroll content** also reachable via paginated, linkable URLs (don't hide content behind JS-only interactions) | Content only revealed on click/scroll may not be rendered/indexed | Disable JS; check whether key content/links still exist |
| 7.7 | **Public doc-page auth nuance**: share-mode 401 means "password required," not "blocked" — ensure truly public docs return 200 to crawlers and don't sit behind a 401/login gate | A public doc behind any auth gate is invisible to all crawlers & AI | `curl` a public share URL anonymously → 200 + content |

---

## 8. International / Canonical Edge Cases

| # | Item | Why it matters | How to verify |
|---|------|----------------|---------------|
| 8.1 | If multilingual: **`hreflang`** reciprocal + self-referencing, valid lang/region codes, with an **`x-default`**; each language version self-canonical (don't canonical translations to one language) | hreflang errors are the most common i18n SEO bug; wrong canonical collapses locales | GSC International Targeting; hreflang validator |
| 8.2 | **Canonical consistency**: canonical, hreflang, sitemap, and OG `og:url` all point to the **same absolute URL** (trailing-slash, case, params normalized) | Conflicting signals confuse consolidation | Cross-check the four for a sample URL |
| 8.3 | **Parameter / tracking URL handling**: UTM and app state params don't create indexable duplicates (canonical to clean URL) | Share links often carry params | Inspect a param'd URL's canonical |
| 8.4 | **Pagination / faceted** views (if doc lists paginate) use clean self-canonicals, not canonical-to-page-1 | Page-1 canonicalization hides deeper content | Check paginated list canonicals |

---

## 9. GEO / Answer-Engine Optimization & LLM Discoverability

> **Framing (2026, primary sources):** Google says generative features use the **same index + E-E-A-T** as Search and that you **do NOT need** llms.txt, special schema, content-chunking, or AI-specific rewriting to appear in AI Overviews/AI Mode. So the highest-leverage "GEO" work is excellent classic SEO + genuinely original, well-structured content. The items below are the *additive* AEO/GEO layer; several are **[CONTESTED]**.

| # | Item | Why it matters | How to verify |
|---|------|----------------|---------------|
| 9.1 **[BLOCKER]** | **E-E-A-T signals present**: named authors with credentials, about/team page, real-world experience/first-hand content, citations to primary sources, clear org identity, `sameAs` to GitHub/npm | ~96% of AI-Overview-cited pages have verifiable E-E-A-T; it's the dominant AI-citation factor and pure classic SEO | Manual audit of author bylines, about page, outbound citations |
| 9.2 | **Answer-first content structure**: the first ~150–200 words directly answer the page's core question; clear self-contained sections; descriptive headings phrased like real questions | Retrieval engines (Perplexity, AI Overviews) weight opening content heavily; extractable passages get cited | Read intro — does it answer or ramble? Check H2s vs likely prompts |
| 9.3 | **Extractable, evidence-dense passages**: specific stats, definitions, comparison tables, numbered lists, named entities, direct quotes — the formats research shows lift AI citation (statistics/quotes/cited-sources) | These are the empirically strongest GEO levers; self-contained facts get quoted | Spot-check for data points, tables, quotable sentences |
| 9.4 | **AI/answer-engine crawler access deliberately decided** in `robots.txt`: allow **retrieval bots you want citations from** (`PerplexityBot`, `ChatGPT-User`, `OAI-SearchBot`, `Google` for AI Overviews) even if you block **training-only** bots (`GPTBot`, `ClaudeBot`, `CCBot`, `Google-Extended`) | Blocking a retrieval bot = zero chance of citation from it. Blocking `Google-Extended` does NOT affect Google Search ranking (confirmed). Decide per bot. | Read `robots.txt`; confirm retrieval bots not accidentally blocked; check server logs for bot hits |
| 9.5 | **Freshness signals**: visible "last updated" dates, `dateModified` in schema, changelog; high-velocity comparison/landscape pages refreshed regularly | Reported citation decay (~14 days) for time-sensitive AI answers; dev-tool landscapes move fast | Check visible dates + `dateModified`; confirm they reflect real updates |
| 9.6 | **Entity/brand presence off-site** (covered in §10): mentions, reviews, and being the answer to "best open-source X" across the web | AI models synthesize from third-party corroboration, not just your own claims | Query ChatGPT/Perplexity/Google AI Mode for "best open-source [category]" and "[tool] alternatives" — is the tool mentioned/cited? Track Mention Rate / Citation Rate / Position |
| 9.7 **[CONTESTED]** | **`llms.txt`** at root: a curated markdown index of key docs/pages | **Honest status (2026):** ~10% adoption; **no major AI provider (OpenAI, Google, Anthropic, Meta) has committed to reading it in production**; Google explicitly says it's not needed and Mueller likened it to the dead keywords meta tag. **Exception:** Anthropic's "Writing for Agents" guidance *does* recommend it for **agent-facing dev-doc/API sites** — which this product (MCP server + npm) plausibly is. **Verdict:** low-cost to ship a correct one for the docs/MCP audience; do **not** expect SEO/AI-citation lift; grade as nice-to-have, never a blocker. | If present: validate it's well-formed markdown (H1 title, blockquote summary, `## ` link sections per spec at llmstxt.org); confirm it's not relied on for indexing |
| 9.8 **[CONTESTED]** | `llms-full.txt` / clean markdown export of doc pages for agent consumption | Same caveats as 9.7; genuinely useful for *your own* MCP/agent UX more than for third-party engines | Fetch and sanity-check |
| 9.9 | **Speakable / clean semantic markup** and no content locked behind JS-only interactions for non-Google AI crawlers (which often don't render JS) | Many AI crawlers ≠ Googlebot; they need server-rendered HTML | `curl` with an AI-bot UA → content present? |

---

## 10. Off-Page Signals (dev-tool-specific: npm / GitHub / backlinks / entity)

| # | Item | Why it matters | How to verify |
|---|------|----------------|---------------|
| 10.1 **[BLOCKER]** | **GitHub repo optimized**: keyword-rich README (the README often outranks your own pages and is a top AI-citation source), accurate description, **topics/tags**, homepage URL → landing, license, clear install/usage, badges | github.com has implicit high authority; a strong README ranks and gets cited; topics drive on-GitHub discovery | View repo; check README depth, topics, homepage link present |
| 10.2 **[BLOCKER]** | **npm package page strong**: complete `package.json` (`description`, `keywords`, `homepage`, `repository`, `bugs`), good README (npm renders it), license; published under findable name (`tela-mcp`) | npm page ranks for the package name and is an authority backlink to your site/repo | `npm view <pkg>`; check fields populated; README renders on npmjs.com |
| 10.3 | **"Awesome list" + directory placements**: relevant `awesome-*` lists, MCP-server registries/directories, Product Hunt, AlternativeTo, "best open-source X" roundups, dev.to/Hacker News presence | High-authority backlinks + the third-party corroboration AI engines synthesize for "best/alternative" answers | Search the tool name + "alternative"/"awesome"/"vs"; audit which lists include it |
| 10.4 | **Backlink profile**: quality > quantity, relevant dev sources, no toxic/spam links; brand mentions even when unlinked | Backlinks remain a core ranking factor; unlinked mentions feed entity/E-E-A-T | Backlink tool (Ahrefs/GSC Links report); review referring domains |
| 10.5 | **Consistent entity identity** across landing, GitHub, npm, X, LinkedIn, docs — same name, logo, description, cross-linked via `sameAs` | Lets Google/AI resolve them to one entity → stronger Knowledge Graph presence | Check `sameAs` in Organization schema vs actual profiles; consistency of naming |
| 10.6 | **GitHub engagement signals** (stars, forks, recent commits, releases, responsive issues) healthy and trending | Social proof + freshness; cited by AI as evidence of an active, trustworthy project | Repo insights; release cadence |
| 10.7 | **Comparison / alternative / integration pages** exist and earn links ("[tool] vs X", "[tool] + [popular host]") | Captures high-intent dev queries and is exactly what AI engines retrieve for recommendations | Inventory check; do these pages exist and rank? |
| 10.8 | **No artificial mention-farming** — Google's 2026 AI guide explicitly warns against manufacturing inauthentic mentions | Inauthentic tactics risk penalties and don't durably work | Manual review of link/mention acquisition tactics |

---

## Scoring Summary Template

| Bucket | Pass | Partial | Fail | N/A | Blocker failed? | Notes |
|--------|------|---------|------|-----|-----------------|-------|
| 1 Crawl/Index | | | | | | |
| 2 On-Page | | | | | | |
| 3 Structured Data | | | | | | |
| 4 Social/OG | | | | | | |
| 5 CWV/Perf | | | | | | |
| 6 Mobile/A11y | | | | | | |
| 7 SPA Pitfalls | | | | | | |
| 8 i18n/Canonical | | | | | | |
| 9 GEO/AEO/llms.txt | | | | | | |
| 10 Off-Page | | | | | | |

**Overall verdict:** any **[BLOCKER]** failed in §§1,2,4,5,7,10 → cap grade at "Needs Work" regardless of other scores. **[CONTESTED]** items (3.6 FAQ, 9.7/9.8 llms.txt) never block and should be weighted low with the dispute noted.

---

### Primary sources grounding this rubric (2026)
- [Google Search Essentials / Crawling & Indexing](https://developers.google.com/search/docs/crawling-indexing) · [JavaScript SEO Basics](https://developers.google.com/search/docs/crawling-indexing/javascript/javascript-seo-basics) · [Mobile-First Indexing](https://developers.google.com/search/docs/crawling-indexing/mobile/mobile-sites-mobile-first-indexing)
- [Google AI Optimization Guide (May 15 2026)](https://developers.google.com/search/docs/fundamentals/ai-optimization-guide) · [announcement](https://developers.google.com/search/blog/2026/05/a-new-resource-for-optimizing) · ["AEO/GEO is still SEO" coverage](https://www.searchenginejournal.com/googles-new-ai-search-guide-calls-aeo-and-geo-still-seo/575026/)
- [Structured Data Search Gallery](https://developers.google.com/search/docs/appearance/structured-data/search-gallery) · [HowTo/FAQ rich-result changes](https://developers.google.com/search/blog/2023/08/howto-faq-changes) · [FAQ rich results dropped (2026)](https://searchengineland.com/google-to-no-longer-support-faq-rich-results-476957)
- [INP (web.dev)](https://web.dev/articles/inp) · [INP became a CWV — Mar 12](https://web.dev/blog/inp-cwv-march-12) · [CWV threshold definitions](https://web.dev/articles/defining-core-web-vitals-thresholds)
- [llms.txt spec](https://llmstxt.org) · [State of llms.txt 2026](https://presenc.ai/research/state-of-llms-txt-2026) · [Anthropic "Writing for Agents" llms.txt guidance] · Mueller "comparable to keywords meta tag" (Reddit, via [getpassionfruit summary](https://www.getpassionfruit.com/blog/should-i-create-an-llms.txt-file-google-s-2026-guidance-explained))
- [AI crawler control / robots.txt for GPTBot, ClaudeBot, PerplexityBot, Google-Extended (2026)](https://nohacks.co/blog/ai-user-agents-landscape-2026)
- [GEO best-practices 2026](https://searchengineland.com/mastering-generative-engine-optimization-in-2026-full-guide-469142) · [OG/Twitter image specs 2026](https://www.krumzi.com/blog/open-graph-image-sizes-for-social-media-the-complete-2026-guide) · [dev-tool off-page/GitHub authority](https://digispot.ai/blog/github-backlinks)