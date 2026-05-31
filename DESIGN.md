<!--
  DESIGN.md — the locked design contract for the tela marketing landing page.
  Source of truth that stops drift back to generic defaults. Re-read every session.
  Pairs with tokens.css (→ landing/src/styles/tokens.css at scaffold time).
-->

# Design Contract — tela landing

## Aesthetic Direction
- **Tone:** Refined developer-tool — Linear / Vercel tier. Dark-first, precise, engineer-credible. Not loud, not editorial, not warm.
- **One-line intent:** A near-black indigo-ink canvas where the real product UI and real code are the heroes, and a faint **woven grid** — tela's loom — runs under everything, lighting up only when an agent writes to the wiki.
- **Brand origin:** For small technical teams already running coding agents (Claude Code, Cursor) who refuse to hand their docs to closed SaaS. The feeling is a quiet, confident dev tool you'd trust with your data — the register of the terminal and the editor, not the marketing deck. *tela* = woven cloth; the site is literally built on a grid you can see.

## Negative Constraints (never — the anti-slop fingerprint)
- No Inter / Roboto / Arial / system-ui / Space Grotesk (Geist family only; Inter is the *product* font, deliberately not reused here).
- No purple-to-pink or generic SaaS gradient blob; no glowing mesh orb behind the hero. Atmosphere is a **single Resend-style diagonal light sweep** + the woven grid, nothing else.
- No centered-hero + single-CTA cliché. Hero is **left-aligned, asymmetric**: copy left, the agent-write signature moment right.
- No three-icon-box feature grid with cute line icons. Pillars are **text-led cards with a code/mono detail**, not iconography.
- No rounded-everything (crisp 6–10px radii; the woven grid and rules are 0px). No flat `0.1` black shadows — depth shadows + one earned indigo glow.
- No stock isometric illustrations, no fake dashboards, no fabricated customer logos, no "trusted by thousands". Real screenshots, real tool names, real `.mcp.json`.
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
- **Where it appears:** hero background (dim, behind the copy), as **section dividers** (a single lit thread-row between sections), and inside the hero wow moment.
- **Hero wow moment — "the agent weaving a page" (ties to CONTENT.md §1):** Left card = a compact MCP **tool-call** (`create_page` / `update_page` / `search`, real catalog names, real `tela_pat_…` shape, no fake data) typing out as a terminal/tool-call card. Right = the **tela editor** with the woven grid faintly visible; as the call "commits," **threads in the grid light up and resolve left-to-right into the page's markdown lines** materializing in the editor — the weave literally *weaves the page into being*. Must read in <5s as *"the AI is editing the wiki, not chatting about it."* A live "writing" dot uses `--live-dot` (cyan). Loops on a `--dur-weave 3200ms` timeline.
- **Build flags:** Hand off to the `wow` skill. This single moment is **exempt from page-level motion throttling**; everything else stays subtle. `prefers-reduced-motion` → render the **final resolved frame** (tool-call done + page present + grid static), no animation.

## Spacing & Layout
- Base grid **4px**; `--col-max 1200` (tight dev-tool frame, not 1240+ editorial); `--section-y 120` vertical rhythm; prose `--measure 66ch`.
- **Composition:** asymmetric, left-anchored. Hero = copy-left / signature-right split. The **agent/MCP section is the visual centerpiece** (Tier 1): full-bleed-ish, the `.mcp.json` code block large and crisp, the 8-of-17 tool catalog grouped by scope (`read`/`write`/`admin`) as mono tags. Product UI (§4) and code (§2, §6) are framed first-class: screenshots in a `--radius-lg` frame with `--shadow-frame`; code in `--radius-md` wells (`--code-bg`) with indigo keyword / cyan string tokens. Pillars (§3) = three text-led cards, not icon boxes. Final CTA (Tier 1) re-lights the woven grid.

## Motion
- **Signature moment:** the woven page-write (above) — the one orchestrated, high-impact gesture.
- Everything else: subtle, fast, compositor-only — **transform/opacity only**; section reveals `--dur-slow` with `--ease-out`; hovers `--dur-fast`. UI transitions < 300ms.
- **Reduced-motion mandatory** — global guard in tokens base layer; the signature moment self-rechecks and renders its resolved static frame.

## Backgrounds / Atmosphere
- Near-black indigo-ink void + a **single faint diagonal light sweep** (Resend-style, one gradient) + the **woven grid** (generated CSS/SVG, never an image). No mesh, no blob, no grain-for-grain's-sake.

## Components
- Base: `@shadcn` (owned, tela's `ui/` convention) + tela token utilities. Premium lane only for the hero signature (the `wow` handoff).
- **Reuse before creating.** Code blocks, tool-tags, proof tiles, and the screenshot frame are owned primitives referencing **semantic tokens** — never raw hex/px.

## Tokens
- Source of truth: `tokens.css` (→ `landing/src/styles/`), Tailwind v4 `@theme`, OKLCH only.
- Three tiers: primitive (ink/text/indigo/cyan ramps) → semantic (`--bg`, `--fg`, `--brand`, `--accent`, `--border`, weave tokens) → component (`--cta-*`, `--code-*`, `--nav-*`, `--pill-*`). Components reference **semantic** tokens, never raw values.
- One knob: `--brand-hue: 277`. Rare secondary `--accent-hue: 200`.
