<!--
  DESIGN.md — the locked design contract for the tela marketing landing page.
  Source of truth that stops drift back to generic defaults. Re-read every session.
  Pairs with tokens.css (→ landing/src/styles/tokens.css at scaffold time).
-->

# Design Contract — tela landing

> 2026-06-27 REPOSITION — THE LOOP: content thesis is now a co-headline **loop**: the team
> wiki that **documents itself** (Atlas writes & refreshes it from your sources) AND that
> **your AI reasons over** (semantic retrieval + a real MCP connector inside Claude & ChatGPT)
> — one continuous system, not two features. The aesthetic, tokens, and woven-grid signature
> are UNCHANGED; this is an evolution of the contract, not a redesign. What changed:
> (1) the old "agent + retrieval co-centerpieces" framing is replaced — **§2–§3b is now ONE
> composed Tier-1 act** read as a single source→write→index→answer→reason flow (Atlas writes →
> retrieval is the shared engine → ask answers → the agent layer is the external half), with
> **Atlas and the agent layer at equal headline weight** and retrieval as the connective tissue
> between them. (2) The signature moment evolves from "agent writes the wiki" to the **FULL
> LOOP** — Atlas weaves a *source* into the cloth (a covered page), then the agent reasons over
> it. The woven-loom motif is now thematically literal: Atlas weaves your sources into the wiki.
> (3) New visual primitive: the Atlas **coverage panel** (score + named gaps + `file:line`
> citations) — the killer dev-credible proof. (4) Self-host stays demoted (a quiet line in
> Security). Sources are **git + Jira only** — never a logo wall, never imply a catalog.
> NEVER render the stale facts "SQLite / FTS5 / single binary" anywhere in chrome, code blocks,
> or captions.

## Aesthetic Direction
- **Tone:** Refined developer-tool — Linear / Vercel tier. Dark-first, precise, engineer-credible. Not loud, not editorial, not warm.
- **One-line intent:** A near-black indigo-ink canvas where the real product UI and real code are the heroes, and a faint **woven grid** — tela's loom — runs under everything, lighting up only at the loop's two beats: when **Atlas weaves a source into the cloth** (writes a covered page) and when an **agent reasons over** that page. The loom is now thematically literal: *tela* weaves your sources into the wiki.
- **Brand origin:** For small technical teams already running AI agents (Claude, ChatGPT, Cursor, Claude Code) who want those agents to actually search and write their shared docs. The feeling is a quiet, confident dev tool you'd trust your team's knowledge to — the register of the terminal and the editor, not the marketing deck. *tela* = woven cloth; the site is literally built on a grid you can see.

## Negative Constraints (never — the anti-slop fingerprint)
- No Inter / Roboto / Arial / system-ui / Space Grotesk (Geist family only; Inter is the *product* font, deliberately not reused here).
- No purple-to-pink or generic SaaS gradient blob; no glowing mesh orb behind the hero. Atmosphere is a **single Resend-style diagonal light sweep** + the woven grid, nothing else.
- No centered-hero + single-CTA cliché. Hero is **left-aligned, asymmetric**: copy left, the agent-write signature moment right.
- No three-icon-box feature grid with cute line icons. Pillars are **text-led cards with a code/mono detail**, not iconography.
- No rounded-everything (crisp 6–10px radii; the woven grid and rules are 0px). No flat `0.1` black shadows — depth shadows + one earned indigo glow.
- No stock isometric illustrations, no fake dashboards, no fabricated customer logos, no "trusted by thousands". Real editor mocks, real tool names, the real connector URL + remote-transport config (NOT an npx command).
- **No fabricated integration/source logo wall.** Atlas reads **git + Jira ONLY** ("more coming"). Show exactly those two as quiet mono source labels (`git` / `jira` with a real repo name / Jira key) — never a grid of vendor logos, never an "any source / your whole stack" montage that implies a catalog.
- **No identical stacked feature sections.** The AI act is ONE composed flow in three beats (Atlas writes → Ask & search find/answer → Agents reason over it externally). Do not render Atlas, Ask & search, and Agents as sibling cards of equal styling stacked vertically — vary their treatment so the flow reads as one system.
- **No marketing gauge for coverage.** The coverage panel is honest dev-credible proof (a restrained mono score + named gaps + `file:line` citations), never a glossy speedometer/percentage-dial hero graphic.
- Containers nest ≤ 2 levels. Indigo is **rare and earned** — most of the page is ink + white text; indigo marks the CTA, the live thread, code keywords, the agent-write moment.

## Inspiration  (pulled live via Steel — decomposed, never replicated)
1. **Linear** (`linear.app`) — *the primary anchor.* Pure near-black canvas; oversized, tight-tracked grotesk headline, **left-aligned**; the real product UI bleeds in below the hero fold, rendered crisply; inline mono code chips (`vehicle_state`) sit *inside* prose; an agent/Copilot panel framed as a first-class teammate ("teams and agents"). **Borrow:** the void canvas, left-aligned tight display type, product-UI-as-hero-evidence, code-chips-in-prose, the agent-as-teammate framing. **Reject:** their grey-on-grey near-monochrome restraint — tela earns one indigo.
2. **Resend** (`resend.com`) — near-black hero with a **single diagonal light sweep** across the void (one luminance gesture, not a gradient blob); gradient-stroked announcement pill; ruthless restraint. **Borrow:** the single-light-sweep atmosphere and the bordered eyebrow pill. **Reject:** the high-contrast editorial *serif* display (tela's display is grotesk, per brief).
3. **Supabase** (`supabase.com`) — one **rare accent color** lighting a single headline line (green on "Scale to millions"); a GitHub **star-count chip in the nav** as instant dev-credibility; clean elevation on feature cards. **Borrow:** rare-accent-on-one-line, the GitHub-count nav chip, calm card elevation. **Reject:** centered-everything + the trusted-by logo wall (tela has no logos, and centers nothing).

> Net: Linear's structure + agent framing, Resend's atmospheric restraint, Supabase's dev-credibility chips — recolored to tela's indigo and stamped with the woven-grid motif neither of them has.

## Typography
- **Body & Display:** **Geist** (Vercel) — sharper apertures + tighter rhythm than Inter, engineer-credible, and deliberately distinct from the product's Inter. Display = same family at weight 600–700 with `--tracking-display -0.025em` (Linear/Vercel one-family move). No editorial serif.
- **Mono:** **Geist Mono** — load-bearing; code blocks (`.mcp.json`, tool catalog, `make up`) are first-class. Same metrics as Geist Sans → seamless code-in-prose and mono kickers/eyebrows.
- **Scale:** explicit ~1.25 modular, base **16px** (dev-tool density, not editorial 18); hero display `--text-5xl` 72px; ≥3× jump from body to display.
- **Weights:** extremes — 400 body / 600–700 display & headings; mono 400/500. Mono uppercase kickers at `--tracking-kicker 0.16em`.

## Color & Theme
- **Brand hue:** `--brand-hue: 277` (indigo). Light brand = `#4f46e5` *exactly* (oklch 0.51 0.23 277) — continuity with the product app/favicon/tokens.
- **Theme:** **Dark is primary/default** (`:root`), driven by `[data-theme]` to match tela's product runtime switch; `[data-theme="light"]` fully defined; OS-preference light honored only when no theme is forced. Justification: the product's home register is dark dev-tool; `[data-theme]` keeps parity with the app's theming contract.
- **Roles (dark):** bg `ink-000` near-black indigo void · surface/sunk/hi = elevation scale · fg cool near-white · brand indigo (rare, earned) · accent cold cyan (`--accent-hue 200`) for link/info/**live-dot** only — never a second brand.
- **Restraint rule:** indigo appears only on the CTA fill, the live/agent-write thread, code keyword/string tokens, and the one accented hero word. Everything else is ink + text. Contrast floors verified (text ≥ 4.5, UI ≥ 3) — see tokens.css header.

## Signature woven-grid device  (the carved-out moment — exempt from page motion throttling)
- **The motif:** `tela` = loom/cloth. A faint **woven grid** of interlaced warp/weft threads (`--weave-thread` lit / `--weave-ground` dim, `--weave-cell 28px`). Rendered as a **CSS layered-gradient weave** (two repeating-linear-gradients offset to read as over/under interlacing) or a tiled SVG `<pattern>` for crisper threads — static, near-invisible, technical not craftsy.
- **Where it appears:** hero background (dim, behind the copy), as **section dividers** (a single lit thread-row between sections), inside the hero wow moment, and — keyed to the Atlas section — as the cloth a source resolves into.
- **Hero wow moment — "THE FULL LOOP: source → covered wiki → agent reasons over it" (ties to CONTENT.md §1 hero):** the centerpiece, evolved from "agent writes the wiki" to the *whole loop*. The woven-loom motif is now thematically perfect — **Atlas weaves your sources into the cloth (the wiki).** Read it in **three beats, left→right, in under 5s**:
  1. **Source (in):** a single quiet **source node** at the left — a `git` repo node (a real repo URL/name) or a `jira` project key, as mono detail. Not a logo wall; one named source.
  2. **Weave → covered page:** threads from the source feed into the woven grid; the **warp/weft threads light up and resolve left-to-right into a freshly written tela page** — real-feeling cited markdown lines, each carrying a `file:line` citation, and a small **coverage badge** (`coverage 92%`) settling in the page's corner. The weave *literally writes the page from the source.* This is the "documents itself" half made visible.
  3. **Agent reasons over it (out):** a compact chat-style turn — `claude · mcp` / `chatgpt · mcp` label — shows an agent calling **`semantic_search`** over that very page and **citing it back** (the citation resolves to a line in the page just written). This closes the loop: the weave wrote the page; the AI now reasons over it.
  - Real names throughout (a repo URL, real tool names, a `tela_pat_…`/connector shape) — **no fake data, no fabricated integration logos.** A live "writing"/"live" dot uses `--live-dot` (cyan). Must read as *"the source becomes a covered wiki, and the AI reasons over it — one loop,"* never "an AI chatting about docs." Loops on a `--dur-weave 3200ms` timeline (extend if the three beats need it; keep each beat legible, not rushed).
- **Build flags:** Hand off to the `wow` skill. This single moment is **exempt from page-level motion throttling**; everything else stays subtle. `prefers-reduced-motion` → render the **final resolved frame** (source node + the covered page present with its `coverage 92%` badge + the agent's cited `semantic_search` turn + grid static), no animation — the static frame alone must still tell the whole loop.

## Spacing & Layout
- Base grid **4px**; `--col-max 1200` (tight dev-tool frame, not 1240+ editorial); `--section-y 120` vertical rhythm; prose `--measure 66ch`.
- **Composition:** asymmetric, left-anchored. Hero = copy-left / signature-right split. **In-page nav** (≤6, scroll order, the AI act leads): `Atlas · Ask · Agents · Compare · Pricing` (Editor dropped from nav — still a full Tier-2 section; Search dropped — semantic search merged into Ask, see below; Pricing is the dedicated-page link, Security stays reachable by scroll). Product UI and code are framed first-class: editor mocks in a `--radius-lg` frame with `--shadow-frame`; code in `--radius-md` wells (`--code-bg`) with indigo keyword / cyan string tokens. The editor section shows the WYSIWYG↔markdown equivalence side by side. Final CTA (Tier 1) re-lights the woven grid. The Security section reads as calm proof tiles, not alarm.
- **The AI act — ONE composed Tier-1 act in THREE beats (2026-06-27 follow-up), not four sibling cards.** This is the structural heart of the reposition. Compose it so a scanner SEES one connected system in source-to-answer order:
  - **§2 Atlas (the "write" half — co-star, equal weight to the agent layer):** a left-anchored act with the **connect-a-source step** (a `git` / `jira` source card with branch/subpath as mono detail), the **coverage panel** (see below) as the killer proof beside a generated tela page (real-feeling cited markdown with `file:line` citations), and a quiet **freshness/drift indicator** (a "behind upstream → regenerated" state, `--live-dot` cyan for the live beat). Honest mechanism, not hype; real repo name / Jira key; never fake data or a source-logo wall.
  - **§3 Ask & search (the "find + answer" half — tela's first-party AI):** ONE section, two cards side by side — a **Search** card (query → ranked sections with mono `heading path`, snippet; "matched on meaning") and an **Ask** card (question → written answer with inline `[n]` citations → cited source pages). Below them a single **"one engine" line** names the mechanism (hybrid keyword + `pgvector`, heading-aware chunks), then a light two-door strip (for your team / for your agent). **Semantic search is NOT its own section** — it's the engine shown inside this beat (it's a mechanism, not an outcome, and it overlapped Ask). Real UI, **no JSON, no raw scores** on the user-facing surface.
  - **§3b The agent layer / MCP (the EXTERNAL half — co-star, equal weight to Atlas):** the connect-flow as 3 crisp steps + the remote-transport config block (real connector URL, `tela_pat_…`, NO npx) + the ~9-of-39 tool catalog grouped by scope `read`/`write`/`admin` as mono tags (the agent's meaning tool is `research`, not the retired `semantic_search`). The strong "works where you already work" beat that closes the act.
  - **Weighting law:** Atlas and the agent layer get the two largest, most prominent treatments (the named halves of the headline); Ask & search sits between them at a notch below. Do NOT stack equal-styled sibling blocks.

## Atlas section & the coverage panel  (new — the killer proof primitive)
- **Atlas section (§2) visual spec — co-star, equal weight to the agent layer:**
  - **Connect a source:** a single quiet **source card** — `git` or `jira` glyph as a mono kicker, a real repo name (e.g. `github.com/zcag/tela`) or a Jira key (e.g. `PROJ`), with **branch / subpath as mono detail** (`branch main · /backend`). Two source types shown, never a vendor-logo grid. Optional include/exclude globs as muted mono chips.
  - **The covered page:** beside the source, a generated tela page rendered in the `--radius-lg` editor frame — real-feeling cited markdown (the deploy/incident theme, consistent with §3/§3a), each grounded line carrying a `file:line` citation chip. Tagged quietly `generator=atlas`.
  - **Freshness / drift indicator:** a restrained status line — a `--live-dot` (cyan) beat plus a state like `behind upstream → regenerated 4m ago` or `drift detected · scheduled daily`. Honest mechanism (cheap ~15-min probe + scheduled regen), never "always up to date / instant".
  - **One-loop tie-in:** a small mono line that `atlas_run` / `atlas_run_status` are MCP tools — Atlas sits in the loop with the agent layer, not off to the side.
- **The coverage panel (NEW owned primitive — first-class, restrained, dev-credible — NOT a marketing gauge):** this is Atlas's differentiator made visible. Three parts, all referencing **semantic tokens only**:
  1. **Coverage score** — a big **mono** number (`92%` / `coverage 92%`), optionally a thin **ring** (a single-stroke arc, `--brand` fill on `--border` track — minimal, not a glossy speedometer). One earned indigo accent permitted here (this is a proof moment).
  2. **Named-gap list** — a short list of what the docs *didn't* cover, each row `kind · name · file:line` in mono, e.g. `route · POST /api/atlas/run · atlas_projects.go:142` / `env · TELA_RAG_EMBED_URL · config.go:88`. Muted, scannable, honest — the gaps are a feature, not a flaw.
  3. **Citation chips** — inline mono chips that resolve to `file:line` (e.g. `atlas_projects.go:142`), visually the same family as the code-keyword treatment; they read as validated provenance, not decoration.
  - **Restraint:** greyscale-first; the score's single indigo accent is the only earned color. Crisp `--radius-md`, no glow, no gradient. It must read as an engineer's audit readout, not a dashboard widget. Reuse the existing code-well / mono-chip tokens; add component tokens only if a semantic one doesn't already cover it (never raw hex/px).

## Motion
- **Signature moment:** the woven page-write (above) — the one orchestrated, high-impact gesture.
- Everything else: subtle, fast, compositor-only — **transform/opacity only**; section reveals `--dur-slow` with `--ease-out`; hovers `--dur-fast`. UI transitions < 300ms.
- **Reduced-motion mandatory** — global guard in tokens base layer; the signature moment self-rechecks and renders its resolved static frame.

## Backgrounds / Atmosphere
- Near-black indigo-ink void + a **single faint diagonal light sweep** (Resend-style, one gradient) + the **woven grid** (generated CSS/SVG, never an image). No mesh, no blob, no grain-for-grain's-sake.

## Components
- Base: `@shadcn` (owned, tela's `ui/` convention) + tela token utilities. Premium lane only for the hero signature (the `wow` handoff).
- **Reuse before creating.** Code blocks, tool-tags, proof tiles, the screenshot frame, and the new **coverage panel** + **source card** are owned primitives referencing **semantic tokens** — never raw hex/px.
- **Coverage panel** (Atlas's proof primitive): score + named-gap list + `file:line` citation chips — spec above. Reuse the code-well / mono-chip tokens; greyscale-first with one earned indigo on the score.
- **Pricing card — Atlas row:** Atlas reads as a **flagship included ability** — render an **Atlas-as-flagship band above the plan cards** (the upgrade reason, prominent). Each plan card then gains a dedicated **"Atlas sources" row** (Free `1` · Plus `5` · Team `20` · Enterprise `unlimited`) — spec the card so this extra metered row sits cleanly alongside the existing limit rows (spaces / pages / storage / AI answers), not bolted on. The **price treatment is structurally UNCHANGED** — the card layout that held the dollar amount and the limit rows stays as is; we're only adding the Atlas-flagship band and the Atlas-sources row. (Dollar values themselves are CONTENT.md's domain — it currently carries `[PRICE TBD]` placeholders for the paid tiers; the card must hold whatever lands without redesign.) The recommended **Team** card keeps its single earned-indigo treatment — the only indigo card; the Atlas-flagship band is the section's other earned-indigo accent.

## Tokens
- Source of truth: `tokens.css` (→ `landing/src/styles/`), Tailwind v4 `@theme`, OKLCH only.
- Three tiers: primitive (ink/text/indigo/cyan ramps) → semantic (`--bg`, `--fg`, `--brand`, `--accent`, `--border`, weave tokens) → component (`--cta-*`, `--code-*`, `--nav-*`, `--pill-*`). Components reference **semantic** tokens, never raw values.
- One knob: `--brand-hue: 277`. Rare secondary `--accent-hue: 200`.
