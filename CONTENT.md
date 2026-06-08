<!--
  CONTENT.md — the LOCKED content/messaging contract for the tela marketing landing page.
  Source of truth for WHAT the page says and HOW it's phrased. Copy below is final and
  shippable — the build uses it verbatim (copywriting + voice skills enforce it).
  Content hierarchy here drives the visual hierarchy in DESIGN.md.
  STATUS: LOCKED. Repositioned 2026-06 around the RAG + MCP thesis (see header note).

  2026-06 REPOSITION (agreed with the user):
  - THESIS now: "RAG + MCP = superpowers on your data." The biggest win is that your
    team's knowledge becomes something your AI can actually reason over — semantic
    retrieval (RAG) + a real read/write API (MCP) — usable right inside Claude and
    ChatGPT, not just a local CLI proxy.
  - Self-host is NO LONGER a headline pillar. It survives as a short reassurance +
    "you can even self-host" escape hatch. The hosted instance is the primary path.
  - Security / org is the trust pillar that replaces the old self-host pillar.
  - FACT CORRECTIONS (the app changed; the old copy was stale):
      * Storage is PostgreSQL + pgvector — NOT "SQLite". NOT "single binary, no Postgres".
      * Keyword search is Postgres native full-text (tsvector + GIN, ranked) — NOT "FTS5".
      * MCP is a remote Streamable-HTTP server with OAuth — NOT just a local npx proxy.
      * 20 MCP tools — NOT 17.
  - These corrections are HARD: never reintroduce "SQLite", "FTS5", or "single binary".

  2026-06 COPY REFINEMENT (post-repositioning, agreed with the user):
  - Voice is PLAIN and capability-first — state what tela does; no story/sales framing
    ("Stop pasting…", "Made a mess?"), no cute tile labels ("Agents on a leash"), no hype.
  - User-facing VISUALS show real UI (a chat exchange, a search box) — NEVER JSON, tool-call
    code, or relevance scores. Users never see JSON, so the page doesn't either.
  - The agent angle leads with placement: "Already in Claude and ChatGPT" (tela goes to where
    your AI already is, vs a chat bolted into a wiki you'd have to open).
  - The landing COMPONENTS hold the authoritative final word-level wording; the section copy
    below captures intent/direction and may trail the components by a refinement pass.
-->

# Content Contract — tela  (LOCKED)

One-page marketing landing page. Standalone, anchored sections. Hosted instance + product: https://tela.cagdas.io

---

## Positioning  (Dunford — 5 components)

- **Competitive alternatives:** Notion / Confluence (closed SaaS wikis — proprietary block store, "AI" bolted on as a chat sidebar, no real agent read/write); a folder of markdown in a git repo + grep; Obsidian/Logseq (single-player, local); "we keep docs in Slack/Google Docs and can't find anything." For the agent angle specifically, the alternative is *pasting context into the chat by hand every session* and hoping the model finds the right doc by keyword.
- **Unique attributes (+ proof):**
  - **Your agents search your docs by meaning, not just keywords.** tela chunks every page (heading-aware), embeds it, and serves **hybrid retrieval** — keyword (Postgres full-text) and vector similarity (pgvector) fused with reciprocal-rank fusion. Agents call `semantic_search` and `read_chunk` to pull the *right section*, with citations, instead of the whole document. *Proof: the RAG service is open (`internal/rag`); `semantic_search` is a live MCP tool.*
  - **A real remote MCP server — usable inside Claude and ChatGPT.** Not a local CLI shim. `https://tela.cagdas.io/api/mcp` is a Streamable-HTTP MCP server with OAuth 2.1 sign-in (one click, no token to paste) and **20 scoped tools** that wrap the same API the UI uses. Submitted to the Claude and ChatGPT connector directories. *Proof: the OAuth + tool surface is open (`internal/api/mcp*.go`); the connector signs you in with your tela account.*
  - **Markdown is canonical forever — under a block editor that feels like Notion.** `pages.body` is plain markdown text; there is no block table, no proprietary format. The editor is full block-editing — drag-to-reorder, slash menu, turn-into, callouts, tables, diagrams — and every block operation round-trips straight back to clean markdown. *Proof: "no block table" is an architectural rule; drag = a markdown line reorder; bulk import/export is first-class.*
  - **Secure and team-shaped out of the box.** Single sign-on (WorkOS), email-verified accounts (Argon2id), organizations and sub-team groups, per-space roles with hard invariants, and scoped API keys that are HMAC-stored, expiring, space-pinnable, and fully audited. *Proof: the access model is documented (`docs/access-model.md`) and the auth/key code is open.*
  - **Real multiplayer + comments that survive edits.** Live collaborative editing over Yjs, rebased onto the canonical markdown on save; comments anchor to a `{prefix, exact, suffix}` text window so they don't drift when the doc is reflowed. *Proof: collab transport in `lib/collab`; anchoring model in architecture.md.*
- **Value themes (4):** (1) Your AI reasons over your team's knowledge — by meaning, with citations. (2) It works where you already work — inside Claude and ChatGPT. (3) Your knowledge stays plain markdown you can take with you. (4) Secure, team-shaped, hosted for you — or self-hosted if you want.
- **Who cares most (ICP):** Small-to-mid technical teams and AI-forward builders who already work *with* agents (Claude, ChatGPT, Cursor, Claude Code) every day, want a shared knowledge base those agents can actually search and write, and are tired of re-pasting context and getting keyword-miss answers. Circumstance: they've put agents on real work and hit the wall where the agent has no durable, searchable, shared memory of the team's docs.
- **Market category:** "Agent-native team wiki." The familiar frame is *team wiki*; the wedge is *your AI can reason over it* — semantic retrieval + MCP, from inside the chat apps. **Style:** new-category wedge inside a known category — lead with the new thing (AI that reasons over your docs), anchor on the known thing (wiki).
- **Why now:** Agents went mainstream in 2025–26 and MCP became the default way to give them tools and data; remote MCP connectors landed in Claude and ChatGPT. Teams now have agents in the browser but no shared place those agents can semantically retrieve and persist team knowledge. tela is that place.

## The customer  (JTBD + VPC)

- **Job story:** When my team is running AI agents on real work, I want a shared wiki my agents can *search by meaning and write back to* — from inside Claude or ChatGPT, without me hand-pasting context — so the agent reasons over our actual docs and the next session picks up where the last left off.
- **Ranked pains (top → nice-to-have):** the agent can't see our knowledge, so I re-paste context constantly · keyword search misses the doc that actually answers the question · "AI" in our wiki is a shallow chat sidebar, not real retrieval + write access · the agent lives in a CLI, not the Claude/ChatGPT app my team uses · docs locked in a proprietary block format · I don't want a free-for-all on who can read what.
- **Ranked gains (top → nice-to-have):** an agent that retrieves the right section by meaning and cites it · read/write access from the chat app we already use · knowledge in plain markdown we own · SSO, roles, and an audit trail so sharing is safe · real-time multiplayer for the humans · take-it-with-you export, and self-host if we ever want to.
- **Forces:** push `agents that can't see our docs; keyword search that misses; re-pasting context every session` · pull `semantic retrieval over our wiki; a real MCP connector in Claude/ChatGPT; markdown we own; SSO + roles` · habit `Notion/Confluence is already set up; markdown-in-git works fine` · anxiety `is the retrieval real or a demo? is my data secure and access-controlled? will I get locked in?`

## Core message & hierarchy

- **ONE core message (hero promise):** *The team wiki your AI actually reasons over — semantic search + a built-in MCP server, usable right inside Claude and ChatGPT, on markdown you own.*
  - Parity test ("who else could say this?"): Notion can't (proprietary store, chat sidebar, no semantic MCP read/write for your agent). A git-repo-of-markdown can't (no retrieval, no live editing, no agent API). Obsidian can't (single-player, no team MCP server). It passes.
- **One-liner (BrandScript):** Technical teams running AI agents struggle because their knowledge base is something the agent can't actually search or write; tela is a markdown team wiki with semantic retrieval and a built-in MCP server, so agents reason over the team's docs — by meaning, with citations — from inside Claude and ChatGPT, instead of starting from zero every session.
- **Supporting pillars (4 — each → one page section):**
  1. **The agent layer — MCP, inside Claude & ChatGPT.** Remote Streamable-HTTP MCP server, OAuth one-click connect, 20 scoped tools over the same API. Proof: open MCP code; live connector.
  2. **Retrieval that reasons — semantic + keyword, fused.** Heading-aware chunking, embeddings, hybrid (pgvector + Postgres full-text) RRF retrieval; agents pull the right section with citations. Proof: open `internal/rag`; `semantic_search` tool.
  3. **A real wiki underneath.** Notion-grade block editing on canonical markdown; live multiplayer; text-anchored comments; history; sharing. Proof: "no block table" rule; collab transport.
  4. **Secure, team-shaped, yours.** SSO, orgs + groups, per-space RBAC, scoped + audited keys; hosted for you, self-host if you want. Proof: open access model + auth code.
- **Awareness stage (dominant visitor):** **Solution-aware → Product-aware.** They want a wiki and they want their agents to use it; they don't yet know one tool does semantic retrieval *and* a real MCP connector in Claude/ChatGPT. → **Page leads with the differentiated outcome + immediate proof** (the agent reasoning over the wiki from Claude/ChatGPT, then the retrieval, then the tools).
- **Objections → reassurance:**
  - "Is the retrieval/agent integration real, or a demo?" → Show the connect flow, the 20-tool catalog, `semantic_search` returning a cited chunk; link the open code and the live connector. *Show, don't claim.*
  - "Is my data secure / access-controlled?" → SSO, email-verified accounts, orgs + groups, per-space roles, scoped + audited keys, SSRF-hardened import. Recent independent-style security pass.
  - "Will I get locked in / lose my data?" → Canonical plain markdown, bulk export, and you can self-host the whole thing. Leaving = copying files.
  - "Is this mature?" → Open code, a live instance you can use now, a versioned connector. Honest about stage (v0) rather than inflating.
- **Cut list (parity / table-stakes — demote or drop):** generic "powerful editor", "beautiful UI", "boost productivity", uptime/perf adjectives without numbers, self-host-as-headline, anything Notion could also say. **Banned facts (stale):** "SQLite", "FTS5", "single binary", "no Postgres".

## Voice  (constant) & Tone (flexes by surface)

**We are precise, dev-credible, and quietly confident — we are NOT hypey, salesy, or vague.**

Write like an engineer wrote it for other engineers: claims are falsifiable, specifics over adjectives, show the thing instead of describing it. Confidence comes from proof, not volume. (The internal shorthand "superpowers on your data" is the *spirit*; in copy it shows up as concrete mechanisms — semantic retrieval, 20 tools, a connector you click — never as the word "superpower".)

| Trait | Do | Don't |
|---|---|---|
| Precise | Name the real thing: "20 MCP tools", "hybrid retrieval", "pgvector", "`tela_pat_`", "`semantic_search`". | Round off into vibes ("tons of integrations", "blazing fast", "supercharge"). |
| Dev-credible | Show the connect flow, the tool catalog, a cited chunk. Link the open code. | Marketing screenshots with fake data; claims you won't show. |
| Confident, low-hype | State the differentiator flatly and let proof carry it. | Exclamation marks, "revolutionary", "superpower", "the future of…". |
| Concrete | Every benefit ties to a mechanism the reader can verify. | Abstract outcomes ("work smarter", "unlock productivity"). |
| Honest | Say "v0", "open", "semantic search needs an embedder endpoint" plainly. | Imply scale/maturity that isn't there; fake logos. |

- **Tone dimensions:** formal↔casual `mid — relaxed but technical, never corporate` · serious↔funny `serious; at most one dry aside` · respectful↔irreverent `respectful, lightly opinionated (closed SaaS is a fair foil)` · matter-of-fact↔enthusiastic `matter-of-fact; enthusiasm shows as specificity, not adjectives`
- **Vocabulary:**
  - **Use:** agent-native · MCP server · semantic search · hybrid retrieval · retrieval-augmented · embeddings · pgvector · markdown-native · canonical markdown · block editing · multiplayer · scoped PAT · single sign-on · orgs / groups / roles · audit · spaces · read, write, and search · your data.
  - **Brand motif (sparing):** *tela* = fabric/canvas/woven cloth. A woven-grid metaphor may appear at most once if it earns its place. Never force it; never explain the etymology in body copy.
  - **Ban — anti-slop kill-list (hard):** revolutionize · seamless · unleash · supercharge · superpower(s) · game-changer · unlock · elevate · empower · leverage · robust · cutting-edge · best-in-class · world-class · next-level · transformative · innovative · "solutions" (noun) · ecosystem · synergy · holistic · curated · turnkey · "in today's fast-paced world" · "take your X to the next level" · "we help teams grow" · "the possibilities are endless" · "not just a wiki, it's…" · em-dash confetti · forced rule-of-three. **Plus the stale-fact ban:** SQLite · FTS5 · "single binary" · "no Postgres".

## Information architecture

- **Site type:** single-page marketing landing (developer-tool tier — Linear/Vercel register). **In-page anchor nav (≤6):** Agents · Search · Editor · Compare · Security. Pinned header: wordmark + GitHub link + theme toggle + primary CTA (`Get started`) + secondary (`Log in`).
- **Page:** one page. **Dominant search intent:** commercial-investigation / navigational ("MCP wiki", "wiki Claude can search", "RAG wiki for agents", "ChatGPT connector wiki", "agent-native wiki", "tela").

## Page plan  (content model — visual hierarchy MUST mirror this priority)

Section order is the narrative arc. Tier = visual prominence (1 = hero/max, 4 = footer/min).

### 1. Hero  — Tier 1  (BAB: the after-state, stated flatly)
- **Eyebrow:** `Agent-native team wiki · semantic search · in Claude & ChatGPT`
- **Headline (H1):** `The wiki your agents reason over.`  (accent the words "reason over")
- **Subhead:** `tela is a markdown team wiki with semantic search and a built-in MCP server — so Claude, ChatGPT, and any agent search your docs by meaning, then read, write, and cite them. Real-time editing for the humans. SSO, scoped access, and an audit trail for the team.`
- **Signature / wow moment (described for the build):** A looping "agent reasoning over the wiki" moment beside the hero. Left: a chat-style turn — a question, then an MCP tool-call (`semantic_search` → a cited chunk, or `update_page`) shown as a compact tool-call card with real catalog names and a `tela_pat_…`/connector shape. Right: the corresponding page in the tela editor; as the call commits, the woven-grid threads light up left-to-right and the page/answer materializes. It must read in under 5 seconds as *"the AI is reasoning over and editing the wiki, not chatting about it."* Real tool names, no fake data. (Carved-out signature moment — hand to the `wow` skill.)
- **Primary CTA:** `Get started` → https://tela.cagdas.io (hosted, free to start).  **Secondary CTA:** `Add to Claude or ChatGPT` → the MCP/connect section (`#agents`).
- **Friction microcopy under CTA:** `Free to start · your markdown, exportable anytime · self-host if you'd rather.`
- 5-second test: a stranger learns *what* (markdown team wiki) + *why it's different* (the AI searches it by meaning and reads/writes it from Claude/ChatGPT) in one glance.

### 2. The agent layer — "Use it inside Claude and ChatGPT."  — Tier 1  (THE STAR; FAB)
- **Purpose:** Prove the headline. The section that wins or loses the page.
- **Headline (H2, question-led for AEO):** `What can an agent actually do with tela?`
- **Answer-first block (40–60 words):** `Everything your team can. tela runs a remote MCP server with 20 scoped tools over the same API the UI uses: search by meaning, read pages and sections, create and update, move, comment, manage spaces. Connect it in Claude or ChatGPT with one OAuth sign-in — no token to paste — or point any MCP client at the same URL.`
- **Show the connect flow (3 steps, real):**
  1. In Claude or ChatGPT, add a connector → `https://tela.cagdas.io/api/mcp`
  2. Sign in with your tela account (OAuth 2.1 — PKCE, no token pasted)
  3. The agent now searches, reads, and writes your wiki — scoped to what your account can see.
  - Caption: `Submitted to the Claude and ChatGPT connector directories.`
- **Show the config for code agents (code block — modern remote transport, NO npx):**
  ```json
  {
    "mcpServers": {
      "tela": {
        "url": "https://tela.cagdas.io/api/mcp",
        "headers": { "Authorization": "Bearer tela_pat_..." }
      }
    }
  }
  ```
  - Caption: `For Claude Code, Cursor, or your own agent. Scoped token, same server.`
- **Tool catalog (compact, real names — pick ~9 to show, group by scope):**
  - `read` — `semantic_search` (meaning + keyword, fused) · `search` (full-text, ranked) · `read_chunk` · `get_page` · `list_pages` · `list_backlinks`
  - `write` — `create_page` · `update_page` (auto-snapshots a revision) · `add_comment` (text-anchored) · `move_page`
  - `admin` — space + key management
  - Footnote: `20 tools total. Keys are scoped read / write / admin and can be pinned to a single space. Interactive result cards render in-chat.`
- **Why-you-care line (FAB benefit):** `Your agent stops starting from zero. It retrieves the right section of the team's docs by meaning, cites it, writes back what it learns — and the next session, human or agent, picks up there.`
- **CTA (transitional):** `See the tool catalog` → `mcp/README.md` (or `/mcp`) on the site/GitHub.

### 3. Retrieval — "Your agents search by meaning."  — Tier 1  (CO-STAR; the biggest single win; FAB)
- **Purpose:** This is the thesis. Keyword search misses; tela retrieves the right section by meaning. Make it concrete.
- **Headline (H2, question-led):** `How does search work?`
- **Answer-first block:** `Two ways, fused. Every page is split into heading-aware chunks, embedded, and indexed. A query runs keyword full-text (Postgres) and vector similarity (pgvector) in parallel, then reciprocal-rank fusion blends them — so a search for "rollback steps" finds the runbook section that never says "rollback". Agents call semantic_search to get ranked chunks with citations, then read_chunk for the full section.`
- **Show it (visual for the build):** a query → ranked **chunk** results, each with its `heading path` (e.g. `Deploy ▸ Production`), a snippet, and a score; a small note that the same retrieval powers the human command palette *and* the agent's `semantic_search` tool. Real-feeling content, never lorem.
- **Two-line "for humans / for agents" split:**
  - **For humans:** `Instant search in the command palette — titles, bodies, and meaning. Jump anywhere.`
  - **For agents:** `semantic_search + read_chunk over the same index — the agent pulls the section that answers the question, with a citation back to the page.`
- **Honesty note (build + copy):** semantic/vector retrieval runs against an embedding endpoint you point tela at (an Ollama-compatible embedder; `mxbai-embed-large` in the live instance). Keyword full-text needs nothing extra. Say this plainly — "semantic search needs an embedder endpoint; on the hosted instance it's already on." Never imply embeddings run with zero setup on a fresh self-host.
- No CTA (momentum carries to the pivot).

### 4. Not-just-AI pivot — "The retrieval only matters because the wiki is real."  — Tier 2  (reassurance pivot)
- **Purpose:** The skeptic just saw the agent + retrieval stars and is bracing for "an AI gimmick on a thin note app." Disarm it: the MCP server and RAG sit *on top of* a real, well-built wiki. One beat, then proof.
- **Headline (H2):** `The retrieval is the new part. The wiki under it is the part that's done.`
- **Body (1–2 lines):** `tela isn't an AI feature looking for a wiki to live in. It's a real markdown wiki — block editing, multiplayer, comments, history, sharing — and the agent layer is one more way in. Take the agents away and you still have a wiki you'd want to use.`
- **Transition line into the editor/showcase:** `Here's what's actually in the box.`

### 5. The editor — "Block editing. Plain markdown underneath."  — Tier 2  (visual proof; the big editor win)
- **Purpose:** Show the new block-editing experience and resolve the apparent tension with "markdown canonical."
- **Headline (H2):** `Edits like a block editor. Saves like a markdown file.`
- **Body:** `Drag a block to reorder it. Hit / for a slash menu — headings, lists, tasks, callouts, tables, code, diagrams, math. "Turn into" changes a block's type in place. It feels like Notion. The difference: every block operation round-trips to clean markdown on disk. Reordering a block reorders markdown lines; there is no block table, no proprietary format to escape from.`
- **Detail chips (real, shipped):** `slash menu` · `drag-to-reorder` · `turn-into` · `callouts` · `tables` · `task lists` · `Mermaid` · `KaTeX math` · `Excalidraw inline` · `paste-to-unfurl`.
- **Visual:** the tela editor with a block being dragged / the slash menu open, beside the same content as raw markdown — the "WYSIWYG ↔ markdown" equivalence shown, not told. Real content.
- No CTA.

### 6. Feature showcase — "What's actually in the box."  — Tier 2  (scannable bento, FAB cells)
- **Purpose:** Prove the pivot. A scannable grid of real capabilities. Cells carry a `real` | `planned` flag; planned cells get a muted style + a small **Planned** tag and never imply they ship today. ~9 cells. As of 2026-06-08 **all cells are `real`** (the graph shipped; the planned Templates cell was replaced by the shipped local-sync cell).
- **Cells (title · one-line description · flag):**
  - **`real`** — **Real-time multiplayer.** `Live cursors and edits over Yjs, rebased onto canonical markdown on save.`
  - **`real`** — **Comments that don't drift.** `Comments anchor to the surrounding text (prefix · exact · suffix), so they stay put when the page is reflowed.`
  - **`real`** — **Block editing → markdown.** `Drag, slash, turn-into — Notion-style editing; the file underneath stays clean markdown.`
  - **`real`** — **History & one-click revisions.** `Every change auto-snapshots. Read any version, diff it, roll back.`
  - **`real`** — **Diagrams & math inline.** `Excalidraw renders in the page; KaTeX math and Mermaid round-trip as markdown.`
  - **`real`** — **Bring your markdown — and take it back.** `Import a directory; export anytime. Plain files in, plain files out.`
  - **`real`** — **Share links, your way.** `Publish any page at a public link with optional password gating and expiry.`
  - **`real`** — **The link graph.** `Wikilinks and backlinks draw a live graph of how pages connect — whole-space or local to the current page.` (shipped 2026-06)
  - **`real`** — **Edit in your own editor.** `Mount a space as a local folder over WebDAV and sync with rclone — Obsidian, VS Code, anything. Round-trips as plain markdown, attachments and all.`
- **Honesty note for the build:** `planned` cells MUST be visually distinct (muted + a "Planned" tag) and never styled identically to shipped cells. Do not promote a planned feature without updating this contract.
- No CTA (momentum carries to the comparison).

### 7. Honest comparison — "How tela compares. Honestly."  — Tier 2  (head-to-head, NOT smug)
- **Purpose:** Beat the reader to the comparison — and concede the one thing each alternative genuinely does better. Conceding builds trust; the synthesis line lands harder.
- **Headline (H2):** `How does tela compare?`
- **Framing line:** `These are all good tools — most of them are why this category exists. Here's where tela is genuinely different, and the one thing each of these still does better.`
- **Row structure (each):** `Alternative` · **`Where tela wins:`** one line · **`What it still does better:`** one line.
- **Rows:**
  - **Notion** — **wins:** `Plain markdown you own, semantic retrieval, and a real MCP connector your agent uses from Claude/ChatGPT — not a closed block store with a chat sidebar.` · **better:** `Databases, templates, and all-round polish are years ahead. Want a relational workspace, not a wiki? Use Notion.`
  - **Confluence** — **wins:** `Agents search and write it by meaning, your content is portable markdown, and you can run it yourself — no enterprise install, no per-seat pricing.` · **better:** `Jira integration, granular permissions, and governance at thousand-user scale. Atlassian shops have reasons to stay.`
  - **Obsidian** — **wins:** `Built for a team: live multiplayer, a shared server with SSO and roles, and an agent API — not a single-player vault you sync by hand.` · **better:** `Local-first single-user is its whole point; the plugin ecosystem and graph view are unmatched. Solo? Hard to beat.`
  - **A git repo of markdown** — **wins:** `Semantic + full-text search, block editing, comments, sharing, and an agent connector — over the same markdown, no PR to read a doc.` · **better:** `Pure version control, free, zero infra. If you just want files in a repo, you already have one.`
  - **Notion AI / "AI" wikis** — **wins:** `Retrieval your own agent drives from Claude or ChatGPT, over markdown you can export — not a built-in chatbot you can only use in their app.` · **better:** `One-vendor convenience and a polished in-app assistant, with no embedder to think about. If you only want their chatbot, it's simpler.`
- **Synthesis line (the close):** `None of them put all of it in one place: an agent that retrieves your docs by meaning, a real MCP connector inside Claude and ChatGPT, markdown you own, and live multiplayer — with SSO and an audit trail. That combination is tela.`

### 8. Security & team — "Secure, team-shaped, and yours."  — Tier 2  (reassurance pillar; replaces the old self-host pillar)
- **Purpose:** Make a technical buyer comfortable putting the team's knowledge in. This is the trust pillar. End with the self-host escape hatch — present, not headline.
- **Headline (H2):** `Built for a team you can trust it with.`
- **Body (answer-first):** `Single sign-on (WorkOS), email-verified accounts with Argon2id password hashing, organizations and sub-team groups, and per-space roles — owner, editor, viewer — with hard invariants (a space always has a real owner; a grant can never silently lower someone's access). Agent keys are scoped read/write/admin, expiring, pinnable to one space, stored only as HMAC, and every key request is audited.`
- **Proof chips / tiles (real, shipped):** `SSO (WorkOS)` · `Orgs + groups` · `Per-space RBAC` · `Scoped, audited API keys` · `Argon2id` · `SSRF-hardened import` · `Password-gated share links`.
- **Self-host escape hatch (short, the demoted pillar):** `Prefer to run it yourself? You can. tela is open and self-hostable — Docker Compose, your Postgres, your disk, your markdown. Read exactly what runs before you run it.`  CTA: `Read the docs` → `docs/` / README.
- **Honesty line:** `Orgs are admin-provisioned (not open self-service signup) and social login isn't wired yet — by design for now. The access model is documented and the auth code is open.`

### 8b. Pricing — "Simple plans. Your markdown either way."  — Tier 2  (added 2026-06: tiers shipped)
- **Purpose:** Show the plan ladder without hype. The product now meters per-account tiers (personal + org); pricing makes the ladder legible. The thesis: tiers change *limits*, never the product — same wiki, search, and agent connector on every plan.
- **Headline (H2):** `Simple plans. Your markdown either way.`
- **Lede:** `A personal account and every organization carry their own tier. Tiers only change the limits — the wiki, the search, and the agent connector are the same.`
- **Personal tiers (cards):** `Free` (3 spaces · 100 pages/space · 100 MB) · `Plus` (25 · 1,000 · 5 GB).
- **Organization tiers (cards):** `Free` (10 · 500 · 1 GB · 5 members) · `Team` *(recommended — the one earned-indigo card)* (100 · unlimited pages · 50 GB · 50 members) · `Enterprise` (unlimited everything; `Get in touch`).
- **Every-plan-includes (checklist band — tiers change limits, never features):** `Semantic (RAG) + full-text search · MCP connector for Claude & ChatGPT · Local folder sync over WebDAV · Real-time multiplayer editing · SSO, organizations & per-space roles · Plain markdown you own — export anytime.`
- **Self-host callout:** `Or run it yourself` — `tela is open source and self-hostable — Docker Compose, your Postgres, your disk, your markdown. No seats to buy and no limits but the ones you set.` CTA `Self-host it` → GitHub.
- **Honesty line:** numbers mirror the backend `plans` table (the source of truth); no self-serve billing yet — plans are operator-assigned, and the CTA starts you on the hosted instance free.

### 9. Credibility — "Open. Live. In the directories."  — Tier 3  (transparency as proof; NO fake logos)
- **Headline (H2):** `Why trust it? Don't — read it and run it.`
- **Three proof tiles (transparency, not testimonials):**
  - `Open code` — `Backend, frontend, and the MCP server are open. See exactly what runs.` → GitHub.
  - `Live instance` — `tela.cagdas.io runs the same code. Use it before you commit.` → live link.
  - `Connector you can add` — `A real MCP connector, submitted to the Claude and ChatGPT directories and versioned against the backend.` → `/mcp` docs.
- **Honesty line:** `tela is at v0 and usable today. No fabricated logos, no "trusted by thousands" — just the code, a running instance, a connector you can add, and a spec you can read.`

### 10. FAQ / objections  — Tier 3  (question-led H2s, answer-first prose; NO FAQPage schema)
- `Does it work inside Claude and ChatGPT?` → `Yes. tela runs a remote MCP server with OAuth sign-in; add it as a connector in Claude or ChatGPT (it's submitted to both directories) and your agent searches, reads, and writes your wiki — scoped to your account. Code agents like Claude Code or Cursor point at the same URL with a token.`
- `How is search different from a normal wiki?` → `tela does hybrid retrieval: keyword full-text (Postgres) and vector similarity (pgvector) fused with reciprocal-rank fusion, over heading-aware chunks. Agents get semantic_search + read_chunk, so they retrieve the section that answers the question — not just keyword matches — with a citation.`
- `Do I need to run an embedder?` → `Only for semantic/vector search, and only on self-host — point tela at an Ollama-compatible embedder (the live instance uses mxbai-embed-large). Keyword full-text needs nothing extra.`
- `Is it really markdown, with all that block editing?` → `Yes. The editor is full block editing — drag, slash menu, turn-into, tables, diagrams — but pages.body is plain markdown. There is no block table; reordering a block reorders markdown lines. Import a directory, export anytime.`
- `Can I edit in my own editor — Obsidian, VS Code?` → `Yes. Mount a space as a local folder over WebDAV and sync it with rclone, then edit in any editor. Pages round-trip as plain markdown and non-markdown files (images, PDFs, diagrams) sync too — local folder and tela stay in step both ways.`
- `How do agents authenticate?` → `OAuth 2.1 for the Claude/ChatGPT connectors (one sign-in, no token to paste), or a scoped personal access token (tela_pat_…) for code agents. Keys are read/write/admin, expirable, pinnable to one space, and audited.`
- `Is my team's data access-controlled?` → `Yes — SSO, organizations and groups, and per-space roles (owner/editor/viewer) with hard invariants. Keys are scoped and audited. The access model is documented and open.`
- `Can I self-host it?` → `Yes. It's open and self-hostable with Docker Compose (Postgres + an optional embedder for semantic search). Your data on your disk, your markdown exportable. Self-host is the option, not the requirement — the hosted instance is ready to use now.`

### 11. Final CTA  — Tier 1  (close)
- **Headline:** `Give your agents a wiki they can reason over.`
- **Subhead:** `Start free on the hosted instance, then connect it in Claude or ChatGPT.`
- **Primary CTA:** `Get started` → https://tela.cagdas.io.  **Secondary:** `Add to Claude or ChatGPT` → `#agents` / `/mcp`.
- **Friction microcopy:** `Free to start · markdown you own · self-host whenever you want.`

### 12. Footer  — Tier 4  (junk drawer)
- Wordmark + one-line descriptor: `tela — the agent-native team wiki your AI reasons over.`
- Links: GitHub · MCP / connector · Docs · Live instance (tela.cagdas.io) · Privacy · License.
- Optional, sparing: `tela — Latin for the woven cloth. A grid you build on.` (use once or not at all.)

- **Primary CTA (one, repeated):** `Get started` → https://tela.cagdas.io · friction: `Free to start · markdown you own · self-host whenever you want.`
- **Secondary CTA (repeated):** `Add to Claude or ChatGPT` → `#agents` / `/mcp`.

## SEO & accessibility

- **`<title>`:** `tela — the agent-native team wiki your AI reasons over`
- **Meta description:** `A markdown team wiki with semantic search and a built-in MCP server. Your AI agents search your docs by meaning and read, write, and cite them — right inside Claude and ChatGPT. Real-time multiplayer, SSO, scoped access. Your markdown, hosted or self-hosted.`
- **5-second clarity test (visitor must grasp):** *tela is a markdown team wiki, and its standout is that your AI agents reason over it — semantic retrieval + a real MCP connector inside Claude and ChatGPT.*
- **Headings:** one H1 (the hero promise); section H2s phrased as real queries where natural ("What can an agent actually do with tela?", "How does search work?"). Descriptive anchor text (never "click here"). Meaningful alt on the editor visual and the hero signature moment. Plain-language reading level (~8th grade) despite the technical audience.
- **JSON-LD:** `SoftwareApplication` (name, MCP/semantic-search/markdown description, `applicationCategory`, `operatingSystem`, open-source), plus `SoftwareSourceCode`, `Organization`/`WebSite`. **No FAQPage** schema (FAQ rich results removed 2026) — answer-first prose under question H2s is the durable play. **Remove "SQLite" from the structured-data description.**

## VoC swipe file  (verbatim language to use in copy)

- "the wiki your agents reason over"
- "search your docs by meaning, not just keywords"
- "use it inside Claude and ChatGPT"
- "the right section, with a citation"
- "stops starting from zero every session"
- "edits like a block editor, saves like a markdown file"
- "no block table — reordering a block reorders markdown lines"
- "markdown you own / take it back / leaving means copying files"
- "SSO, roles, scoped and audited keys"
- "submitted to the Claude and ChatGPT connector directories"
- "hosted for you — self-host if you'd rather"
- "read exactly what runs before you run it"
- "use it before you commit"
