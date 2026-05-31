<!--
  CONTENT.md — the LOCKED content/messaging contract for the tela marketing landing page.
  Source of truth for WHAT the page says and HOW it's phrased. Copy below is final and
  shippable — the build uses it verbatim (copywriting + voice skills enforce it).
  Content hierarchy here drives the visual hierarchy in DESIGN.md.
  STATUS: LOCKED. Positioning angle, voice, and section order were agreed with the user.
-->

# Content Contract — tela  (LOCKED)

One-page marketing landing page. Standalone, anchored sections. Public instance: https://tela.cagdas.io

---

## Positioning  (Dunford — 5 components)

- **Competitive alternatives:** Notion / Confluence (SaaS wikis — closed data, no real agent access, an "AI" feature bolted on top); a folder of markdown files in a git repo + grep; Obsidian/Logseq (single-player, local); "we keep docs in Slack/Google Docs and can't find anything." For the agent angle specifically, the alternative is *pasting context into the chat by hand* every session.
- **Unique attributes (+ proof):**
  - **Built-in MCP server, not a chat bolt-on.** Ships `tela-mcp` (published on npm). Agents bearer-auth with a scoped PAT and get 17 tools that wrap the same REST API the UI uses — create / read / update / search / comment / import / manage spaces. Read, write, and admin scopes; keys can be pinned to one space. *Proof: the tool catalog and `.mcp.json` are public in `mcp/README.md`; the code is open.*
  - **Markdown is canonical forever.** `pages.body` is plain markdown text — there is no block model, no proprietary document format. The Milkdown WYSIWYG editor reads and writes markdown directly. *Proof: "no block table" is an architectural rule; bulk markdown import/export is a first-class tool.*
  - **Self-hosted single binary + SQLite.** Go compiles to one binary; storage and full-text search are SQLite + FTS5 — no Postgres, no Elasticsearch, no external search service. Docker Compose, one host port, your data on your disk. *Proof: stack table in the README; `make up`.*
  - **Real multiplayer.** Live collaborative editing over Yjs — concurrent cursors and edits, rebased onto the canonical markdown on save. *Proof: custom WebSocket transport in `lib/collab`.*
  - **Comments that survive edits.** Comments anchor to a `{prefix, exact, suffix}` text window, never to a character offset, so they don't drift when the doc is reflowed. *Proof: anchoring model in architecture.md.*
- **Value themes (4):** (1) Your agents are first-class teammates, not a sidebar. (2) Your knowledge stays in plain markdown you own. (3) One binary you run yourself — no SaaS, no lock-in. (4) Live, fast, and searchable for the humans too.
- **Who cares most (ICP):** Small technical teams, engineers, and AI-forward builders who already work *with* coding agents (Claude Code, Cursor) every day, want a shared knowledge base those agents can actually read and write, and refuse to hand their docs to a closed SaaS. Circumstance: they've started running agents on real work and hit the wall where the agent has no durable, shared memory of the team's docs.
- **Market category:** "Agent-native team wiki." Self-hosted markdown wiki is the familiar frame; *agent-native* is the wedge that makes the value obvious. **Style:** new-category wedge inside a known category (wiki) — lead with the new thing (agents), anchor on the known thing (wiki) so it's instantly legible.
- **Why now:** Coding agents went mainstream in 2025–26 and MCP became the default way to give them tools. Teams now have agents but no shared place those agents can persist and retrieve knowledge. tela is that place.

## The customer  (JTBD + VPC)

- **Job story:** When my team and I are running coding agents on real work, I want a shared wiki my agents can read and write through MCP — without me hand-pasting context every session — so the agent has durable team memory and I keep our docs in plain markdown on my own server.
- **Ranked pains (top → nice-to-have):** agent has no shared memory across sessions → I re-paste the same context constantly · our knowledge base is closed SaaS the agent can't truly reach · "AI" features are a shallow chat bolt-on, not real read/write access · docs locked in a proprietary block format I can't export cleanly · search is slow or needs a separate service · don't want our internal docs living on someone else's servers.
- **Ranked gains (top → nice-to-have):** an agent that reads and writes the wiki itself · knowledge in plain markdown I own outright · runs on my own box with one command · instant full-text search with nothing extra to operate · real-time multiplayer for the human team · public share links when I need them.
- **Forces:** push `agents with no durable memory; closed wiki the agent can't use; re-pasting context every time` · pull `a wiki with a real MCP server; markdown I own; one self-hosted binary` · habit `Notion/Confluence is already set up; markdown-in-a-git-repo works fine` · anxiety `is self-hosting a hassle? is the MCP integration real or a demo? will I lose my data / get locked in?`

## Core message & hierarchy

- **ONE core message (hero promise):** *The team wiki your AI agents can actually read and write — through a built-in MCP server, on markdown you self-host.*
  - Parity test ("who else could say this?"): Notion can't (closed data, chat bolt-on, no self-host). A git-repo-of-markdown can't (no live editing, no real search, no agent API, single-player). Obsidian can't (single-player, no team MCP server). It passes.
- **One-liner (BrandScript):** Technical teams running AI agents struggle with a knowledge base their agents can't actually use; tela gives them a self-hosted markdown wiki with a built-in MCP server, so their agents read, write, and search alongside the team instead of starting from zero every session.
- **Supporting pillars (4 — each → one page section):**
  1. **Agent-native — built-in MCP server.** `tela-mcp` on npm; 17 scoped tools over the same REST API; agents create / read / update / search / comment. Proof: public tool catalog + `.mcp.json`.
  2. **Markdown you own.** Plain markdown is canonical forever — no block model, no proprietary format; bulk import/export. Proof: "no block table" architectural rule.
  3. **Self-hosted, single binary.** Go binary + SQLite/FTS5, Docker Compose, one host port, your server. No SaaS, no external search. Proof: stack table; `make up`.
  4. **Live and fast for humans too.** Yjs multiplayer editing, instant FTS5 search, text-anchored comments, page history, public share links, Excalidraw diagrams. Proof: feature set in architecture.md.
- **Awareness stage (dominant visitor):** **Solution-aware → Product-aware.** They know they want a wiki and they know they want agents to use it; they don't yet know a tool exists that does both. → **Page leads with the differentiated outcome + immediate proof** (the agent-native hook, then show the MCP tools and code).
- **Objections → reassurance:**
  - "Is the agent integration real, or a demo?" → Show the actual `.mcp.json` and the 17-tool catalog; link to the published npm package and the open code. *Show, don't claim.*
  - "Is self-hosting going to be a pain?" → One binary, `make up`, SQLite — no Postgres/Elasticsearch to run. Single host port.
  - "Will I get locked in / lose my data?" → Canonical plain markdown, bulk export, your own disk. Leaving = copying files.
  - "Is this mature?" → Open code, live public instance you can use right now, versioned MCP package. Be honest about stage (v0) rather than inflating.
- **Cut list (parity / table-stakes — demote or drop):** generic "powerful editor", "beautiful UI", "boost productivity", role-based access as a headline (mention inline only), uptime/perf adjectives without numbers, anything Notion could also say.

## Voice  (constant) & Tone (flexes by surface)

**We are precise, dev-credible, and quietly confident — we are NOT hypey, salesy, or vague.**

Write like an engineer wrote it for other engineers: claims are falsifiable, specifics over adjectives, show the code instead of describing it. Confidence comes from proof, not volume.

| Trait | Do | Don't |
|---|---|---|
| Precise | Name the real thing: "17 MCP tools", "SQLite + FTS5", "one binary", "`tela_pat_`". | Round off into vibes ("tons of integrations", "blazing fast"). |
| Dev-credible | Show the `.mcp.json`, the tool table, the `make up`. Link to the open code. | Marketing screenshots with fake data; claims you won't show. |
| Confident, low-hype | State the differentiator flatly and let proof carry it. | Exclamation marks, "revolutionary", "the future of…". |
| Concrete | Every benefit ties to a mechanism the reader can verify. | Abstract outcomes ("work smarter", "unlock productivity"). |
| Honest | Say "self-hostable", "open", "v0" plainly; admit what's early. | Imply scale/maturity that isn't there; fake logos. |

- **Tone dimensions:** formal↔casual `mid — relaxed but technical, never corporate` · serious↔funny `serious; at most one dry aside` · respectful↔irreverent `respectful, lightly opinionated (closed SaaS is a fair foil)` · matter-of-fact↔enthusiastic `matter-of-fact; enthusiasm shows as specificity, not adjectives`
- **Vocabulary:**
  - **Use:** agent-native · MCP server · markdown-native · self-hostable · single binary · canonical markdown · FTS5 / full-text search · multiplayer · scoped PAT · spaces · your server / your data · read and write.
  - **Brand motif (sparing):** *tela* = fabric/canvas/woven cloth. A woven-grid metaphor may appear at most once if it earns its place. Never force it; never explain the etymology in body copy.
  - **Ban — anti-slop kill-list (hard):** revolutionize · seamless · unleash · supercharge · game-changer · unlock · elevate · empower · leverage · robust · cutting-edge · best-in-class · world-class · next-level · transformative · innovative · "solutions" (noun) · ecosystem · synergy · holistic · curated · turnkey · "in today's fast-paced world" · "take your X to the next level" · "we help teams grow" · "the possibilities are endless" · "not just a wiki, it's…" · em-dash confetti · forced rule-of-three.

## Information architecture

- **Site type:** single-page marketing landing (developer-tool tier — Linear/Vercel register). **In-page anchor nav (≤6):** Agents · Features · Compare · Self-host · Search · Get started. Pinned header: wordmark + GitHub link + primary CTA. (Anchor `Features` → §4 showcase; `Compare` → §5 comparison.)
- **Page:** one page. **Dominant search intent:** commercial-investigation / navigational ("MCP wiki", "self-hosted Notion alternative for AI agents", "agent-native wiki", "tela").

## Page plan  (content model — visual hierarchy MUST mirror this priority)

Section order is the narrative arc. Tier = visual prominence (1 = hero/max, 4 = footer/min).

### 1. Hero  — Tier 1  (BAB: the after-state, stated flatly)
- **Eyebrow:** `Agent-native team wiki · self-hosted · open`
- **Headline (H1):** `The wiki your agents can write to.`
- **Subhead:** `tela is a self-hostable, markdown-native team wiki with a built-in MCP server — so Claude, Cursor, and any MCP client read, write, and search your docs as first-class teammates. Live multiplayer for the humans. Your data on your own server.`
- **Signature / wow moment (described for the build):** A live, looping "agent writing into the wiki" moment beside the hero. Left: an MCP client calling a tool (`create_page` / `update_page` / `search`) as a compact terminal/tool-call card. Right: the corresponding page materializing in the tela editor in real time — the woven-grid motif can surface subtly as the page renders in. It must read in under 5 seconds as *"the AI is editing the wiki, not chatting about it."* Real tool names from the catalog, no fake data. (Hand off to `wow` skill — this is the carved-out signature moment.)
- **Primary CTA:** `Self-host it` → repo / quickstart (`make up`).  **Secondary CTA:** `Try the live instance` → https://tela.cagdas.io
- **Friction microcopy under CTA:** `One binary + SQLite. docker compose up. Your server.`
- 5-second test: a stranger learns *what* (self-hosted markdown wiki) + *why it's different* (agents read/write it via MCP) in one glance.

### 2. The agent section — "Your agents get a real API, not a chat box."  — Tier 1  (THE STAR; FAB)
- **Purpose:** Prove the headline. This is the section that wins or loses the page.
- **Headline (H2, question-led for AEO):** `What can an agent actually do in tela?`
- **Answer-first block (40–60 words):** `Everything your team can. tela ships tela-mcp, a published MCP server that gives agents 17 scoped tools over the same REST API the UI uses: create, read, update, and search pages, leave anchored comments, import markdown, and manage spaces. Agents authenticate with a scoped personal access token — read, write, or admin.`
- **Show the config (code block, verbatim from mcp/README.md):**
  ```json
  {
    "mcpServers": {
      "tela": {
        "command": "npx",
        "args": ["-y", "tela-mcp@latest"],
        "env": {
          "TELA_BASE_URL": "https://tela.cagdas.io",
          "TELA_API_KEY": "tela_pat_..."
        }
      }
    }
  }
  ```
  - Caption: `Drop this in your .mcp.json. That's the whole integration.`
- **Tool catalog (compact, real names — pick ~8 to show, group by scope):**
  - `read` — `search` (FTS5 over title + body, snippet-highlighted) · `get_page` · `list_pages` · `list_backlinks`
  - `write` — `create_page` · `update_page` (auto-snapshots a revision) · `add_comment` (text-anchored) · `import_markdown` (bulk)
  - `admin` — space + key management
  - Footnote: `17 tools total. Keys are scoped read / write / admin and can be pinned to a single space.`
- **Why-you-care line (FAB benefit):** `Your agent stops starting from zero every session. It reads the team's docs, writes back what it learns, and the next session — human or agent — picks up where it left off.`
- **CTA (transitional):** `See the tool catalog` → `mcp/README.md` on GitHub.

### 3. Not-just-AI pivot — "The agent only matters because the wiki is real."  — Tier 2  (reassurance pivot)
- **Purpose:** The skeptical reader just saw the MCP star and is bracing for "an AI gimmick stapled to a thin note app." Disarm it: the MCP server sits *on top of* a real, well-built wiki — not the other way around. One beat, then move straight into proof.
- **Headline (H2):** `The MCP server is the part that's new. The wiki under it is the part that's done.`
- **Body (1–2 lines):** `tela isn't an AI feature looking for a wiki to live in. It's a real markdown wiki — multiplayer editing, instant search, comments, history, sharing — and the agent API is one more way in. Take the agents away and you still have a wiki you'd want to use.`
- **Transition line into the showcase:** `Here's what's actually in the box.`

### 4. Feature showcase — "What's actually in the box."  — Tier 2  (scannable grid, FAB cards)
- **Purpose:** Prove the pivot. A scannable grid of real capabilities so the reader stops worrying it's thin. Cards carry a `real` | `planned` flag; planned cards get a small **Planned** tag in the copy and never imply they ship today. Recommend 9 cards: 7 shipped, 2 planned (kept honestly minority so the grid reads as "mostly real, a little roadmap").
- **Section intro (optional H2 already above):** keep the headline; no extra intro line.
- **Cards (title · one-line description · flag):**
  - **`real`** — **Markdown all the way down.** `Every page is plain markdown — no block model, no proprietary format. Grep it, diff it, export it, walk away with it.`
  - **`real`** — **WYSIWYG that writes markdown.** `The Milkdown editor is fully WYSIWYG; the file underneath stays clean markdown. No mode-switching, no export step.`
  - **`real`** — **Real-time multiplayer.** `Live collaborative editing over Yjs — concurrent cursors and edits, rebased onto the canonical markdown on save.`
  - **`real`** — **Instant full-text search.** `SQLite FTS5 over titles and bodies, snippet-highlighted, plus a command palette to jump anywhere. No Elasticsearch to run.`
  - **`real`** — **Comments that don't drift.** `Comments anchor to the surrounding text (prefix · exact · suffix), so they stay put when the page is edited and reflowed.`
  - **`real`** — **History and one-click revisions.** `Every body change auto-snapshots a revision. Read any past version, diff it, roll back in a click.`
  - **`real`** — **Share links, your way.** `Publish any page at a public link with optional password gating — same code, no extra service. Excalidraw diagrams render inline.`
  - **`planned` (Planned)** — **The link graph.** `Backlinks already exist on every page. The graph view that draws them — your wiki as a map you can navigate — is on the roadmap.`
  - **`planned` (Planned)** — **Mermaid, rendered.** `Mermaid blocks are preserved as code today and survive every round-trip. Rendering them inline is on the roadmap.`
- **Honesty note for the build:** the two `planned` cards MUST be visually distinct (muted + a small "Planned" tag) and must never be styled identically to shipped cards. Do not move a planned feature into the shipped set without updating this contract.
- No CTA (momentum carries to the comparison).

### 5. Honest comparison — "How tela stacks up. Honestly."  — Tier 2  (head-to-head, NOT smug)
- **Purpose:** The reader is mentally comparing tela to what they already use. Beat them to it — and concede the one thing each alternative genuinely does better. Conceding builds more trust than a clean sweep; the synthesis line lands harder because the column above it is honest.
- **Format (recommended):** **head-to-head cards**, one per alternative, NOT a giant checkmark matrix. A row-×-alternative grid with green ticks for tela everywhere reads as marketing theatre to a dev audience and forces table-stakes parity rows. Cards let each comparison say one true edge and one honest concession in plain prose. Five cards, equal weight, alternative name as the card title.
- **Headline (H2):** `How does tela compare?`
- **Framing line (honest, confident, not smug):** `These are all good tools — most of them are why this category exists. Here's where tela is genuinely different, and the one thing each of these still does better.`
- **Card structure (each card):** `Alternative name` · **`tela's edge:`** one line · **`They do better:`** one line.
- **Cards:**
  - **Notion** — **tela's edge:** `Your docs are plain markdown on your own server, and agents get a real read/write API — not a closed block store with a chat sidebar bolted on.` · **They do better:** `Notion's databases, templates, and all-round product polish are years ahead. If you want a relational workspace, not a wiki, use Notion.`
  - **Confluence** — **tela's edge:** `One binary you run yourself, in minutes — no enterprise install, no per-seat pricing, and your data stays on your disk.` · **They do better:** `Deep Jira integration, granular permissions, and governance at thousand-user scale. Big orgs that live in Atlassian have reasons to stay.`
  - **Obsidian** — **tela's edge:** `Built for a team: live multiplayer, a server everyone shares, and an agent API — not a single-player vault you sync by hand.` · **They do better:** `Local-first single-user is its whole point, and the plugin ecosystem and graph view are unmatched. Solo? Obsidian is hard to beat.`
  - **A git repo of markdown (or GitHub wiki)** — **tela's edge:** `Live WYSIWYG editing, instant full-text search, comments, sharing, and an agent API — over the same markdown, no pull requests to read a doc.` · **They do better:** `Pure version control, free, zero infra, and Git's history is the real thing. If you just want files in a repo, you already have one.`
  - **Outline** — **tela's edge:** `A built-in MCP server makes agents first-class, and your content is canonical markdown on a single self-hosted binary — no Postgres, no Redis, no separate search.` · **They do better:** `Outline is mature and polished, with real Slack integration and a track record tela doesn't have yet. It's the closest thing to a finished version of this.`
- **Synthesis line (the close — what none of them combine):** `None of them put all of it in one place: an agent-native MCP server, markdown you actually own, live multiplayer, real full-text search — in a single binary you run yourself. That combination is tela.`

### 6. Pillars strip — "Markdown you own · One binary · Live"  — Tier 2  (FAB, three cards)
- **Section intro (optional H2):** `A wiki built on things you can grep, run, and export.`
- **Card A — Markdown is canonical. Forever.**
  `Every page is plain markdown text — no block model, no proprietary format. The Milkdown editor is WYSIWYG; the file underneath is markdown. Bulk-import a directory, export anytime. Leaving tela means copying files.`
- **Card B — One binary. SQLite. Your server.**
  `Go compiles to a single binary; storage and full-text search are SQLite + FTS5 — no Postgres, no Elasticsearch, nothing extra to operate. docker compose up, one host port, your data on your disk.`
- **Card C — Real-time multiplayer.**
  `Live collaborative editing over Yjs: concurrent cursors and edits, rebased onto the canonical markdown on save. Comments anchor to the surrounding text, so they don't drift when the doc is reflowed.`

### 7. Show the product — "This is the editor."  — Tier 2  (visual proof)
- **Headline (H2):** `A fast wiki the humans actually want to use.`
- **Body:** `Spaces with nested pages. WYSIWYG markdown editing. Page history with one-click revisions. Public share links with optional password gating. Excalidraw diagrams inline. Role-based access per space.`
- **Visual:** real screenshot / interactive snippet of the Milkdown editor with a populated space tree and a multiplayer cursor — real content, never lorem.
- No CTA (kept clean; momentum carries to search).

### 8. Search — "Find anything. No second service."  — Tier 2  (question-led, FAB)
- **Headline (H2):** `How does search work?`
- **Answer-first block:** `Instant full-text search runs on SQLite FTS5 in the backend — no Elasticsearch, no external search to run or pay for. The command palette searches titles and bodies with snippet highlights; an Orama index gives client-side fuzzy search per space. Agents hit the same search through the search tool.`

### 9. Self-host / ownership — "Run it yourself in minutes."  — Tier 2  (PAS resolution + risk reversal)
- **Headline (H2):** `Your wiki, on your machine.`
- **Body:** `No SaaS account, no seat pricing, no data on someone else's servers. Clone, set three secrets, and run.`
- **Quickstart (code block, real):**
  ```bash
  cp deploy/.env.example deploy/.env   # set TELA_PUBLIC_BASE_URL + 2 secrets
  make up                              # backend + frontend + Caddy on :8780
  ```
- **Reassurance line:** `Your markdown lives in SQLite on a volume you control. Open code — read exactly what runs before you run it.`
- **CTA:** `Read the docs` → `docs/` / README quickstart.

### 10. Credibility — "Open. Live. Run it yourself."  — Tier 3  (transparency as proof; NO fake logos)
- **Headline (H2):** `Why trust it? Don't — read it and run it.`
- **Three proof tiles (transparency, not testimonials):**
  - `Open code` — `The backend, frontend, and MCP server are open. See exactly what runs.` → GitHub.
  - `Live instance` — `tela.cagdas.io runs the same code. Use it before you install it.` → live link.
  - `Published MCP package` — `tela-mcp ships on npm, versioned against the backend it talks to.` → npm.
- **Honesty line (builds trust with this audience):** `tela is at v0 and self-hostable today. No fabricated logos, no "trusted by thousands" — just the code, a running instance, and a spec you can read.`

### 11. FAQ / objections  — Tier 3  (question-led H2s, answer-first prose; NO FAQPage schema)
- `Is the MCP integration real or a demo?` → `Real. tela-mcp is published on npm with 17 tools over the live REST API. The full tool catalog and the .mcp.json config are in mcp/README.md, and the code is open.`
- `Do I need Postgres or Elasticsearch?` → `No. Storage and full-text search are both SQLite + FTS5 inside the single Go binary. The only services in the stack are the backend, the frontend, and a Caddy proxy.`
- `Can I get my content out?` → `Yes. Pages are canonical plain markdown. Bulk-import a directory and export anytime; there's no proprietary format to convert away from.`
- `How do agents authenticate?` → `Each agent uses a scoped personal access token (tela_pat_…) issued in Settings → API Keys. Keys are read, write, or admin, and can be pinned to a single space.`
- `Is it multiplayer?` → `Yes — live collaborative editing over Yjs, with concurrent cursors and edits rebased onto the markdown on save.`
- `How is this different from Notion or Confluence?` → `Your data is plain markdown on your own server, and agents get a real read/write API through a built-in MCP server — not a chat sidebar bolted onto a closed document store.`

### 12. Final CTA  — Tier 1  (close)
- **Headline:** `Give your agents a wiki they can write to.`
- **Subhead:** `Self-host tela in a few minutes, or open the live instance first.`
- **Primary CTA:** `Self-host it` → repo/quickstart.  **Secondary:** `Try the live instance` → https://tela.cagdas.io
- **Friction microcopy:** `One binary. docker compose up. Your server, your markdown.`

### 13. Footer  — Tier 4  (junk drawer)
- Wordmark + one-line descriptor: `tela — the agent-native, self-hosted markdown wiki.`
- Links: GitHub · Docs · MCP server (npm) · Live instance (tela.cagdas.io) · License.
- Optional, sparing: `tela — Latin for the woven cloth. A grid you build on.` (use once or not at all.)

- **Primary CTA (one, repeated):** `Self-host it` · friction microcopy: `One binary + SQLite. docker compose up. Your server.`
- **Secondary CTA (repeated):** `Try the live instance` → https://tela.cagdas.io

## SEO & accessibility

- **`<title>`:** `tela — the agent-native, self-hosted markdown wiki`
- **Meta description:** `A self-hostable, markdown-native team wiki with a built-in MCP server. Your AI agents read, write, and search your docs as first-class teammates. Live multiplayer, SQLite full-text search, your data on your own server.`
- **5-second clarity test (visitor must grasp):** *tela is a self-hosted markdown team wiki, and its standout is that your AI agents can read and write it directly through a built-in MCP server.*
- **Headings:** one H1 (the hero promise); section H2s phrased as real queries where natural ("What can an agent actually do in tela?", "How does search work?"). Descriptive anchor text (never "click here"). Meaningful alt on the editor screenshot and the hero agent-write animation. Plain-language reading level (~8th grade) despite the technical audience — precise, not dense.
- **JSON-LD:** `SoftwareApplication` (name, MCP/markdown/self-host description, `applicationCategory`, `operatingSystem`, open-source), plus `Organization`/`WebSite`. **No FAQPage** schema (FAQ rich results removed 2026) — answer-first prose under question H2s is the durable play.

## VoC swipe file  (verbatim language to use in copy)

- "the wiki your agents can write to"
- "first-class teammates, not a sidebar / not a chat bolt-on"
- "stops starting from zero every session"
- "read, write, and search your docs"
- "markdown you own / canonical markdown forever"
- "no block model, no proprietary format"
- "leaving tela means copying files"
- "one binary + SQLite — no Postgres, no Elasticsearch"
- "docker compose up. Your server, your markdown."
- "drop this in your .mcp.json — that's the whole integration"
- "read exactly what runs before you run it"
- "use it before you install it"
