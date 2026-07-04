<!--
  ACCEPTANCE.md — frozen acceptance contract for the tela marketing landing page.
  Agreed by planner (content-strategist + design-director) and evaluator (design-reviewer)
  BEFORE Stage 3 build. APPEND-ONLY: tick `passes`, never edit or delete a criterion.
  Gate order is fixed: deterministic rules → visual-diff vs DESIGN.md → trajectory/LLM judge.
-->

# Acceptance Contract — tela landing page (standalone Astro, `landing/`)

## "Done" means (agreed before any build)
- The ONE core message is clear above the fold: **tela is the team wiki your AI agents can read and write — via a built-in MCP server, on markdown you self-host.** A visitor gets "agent-native wiki" in 5 seconds.
- The committed aesthetic is realized: **refined developer-tool, Linear/Vercel tier — dark-first indigo-ink canvas, real product UI + real code as the heroes, a faint woven-grid (tela's loom) signature device.**
- Scope: single-page landing — Hero · Agent/MCP section (centerpiece + signature moment) · "not-just-AI" pivot · Feature showcase (9 cards; `planned` cards visually muted + honest) · Honest comparison (head-to-head cards vs Notion/Confluence/Obsidian/git-repo/Outline, each crediting a genuine concession) · Pillars · Show-the-product · Search · Self-host/ownership · Credibility · FAQ · Final CTA · Footer. (Per CONTENT.md.)

## Deterministic gates — HARD (must pass; can't be charmed) · `npm run gate`
- [x] Render-health: builds, renders, no console errors
- [x] Token conformance: no raw hex / off-token color (computed-style); OKLCH token system only
- [x] Spacing on grid (4px) · type on scale
- [x] Layout invariants @ 360/768/1024/1440: no h-scroll, no overflow, no unintended overlap; touch targets ≥24px
- [x] axe WCAG 2.2 AA clean (4 viewports, score 1.0) · visible focus · `prefers-reduced-motion` honored (signature moment renders resolved static frame)
- [x] Lighthouse budgets (LCP/CLS/TBT·INP) — showcase profile; a11y=1.0, CLS ~0, non-composited-animations=0 (only lab-INP warn, no interactions to measure)
- [x] Anti-slop kill-list returns zero (copy) — bans incl. revolutionize/seamless/unleash/supercharge/game-changer + cila slop list
- [x] Brand continuity: indigo reads as tela's `#4f46e5` family; light theme uses `#4f46e5` exactly

## Visual & message — judge vs DESIGN.md + CONTENT.md (evidence-cited)
- [x] Hero passes the 5-second clarity test ("agent-native wiki")
- [x] Headline specific / falsifiable / differentiated (Notion/git-repo/Obsidian can't say it)
- [x] The agent/MCP section SHOWS what an agent does (real `.mcp.json` + real tool catalog), not tells
- [x] Every section survives "So what?" · one clear primary CTA (`Self-host it`) + secondary (`Try the live instance`)
- [x] Realizes the committed tone (dev-credible, precise, low-hype) · scannable (layer-cake)
- [x] The woven-grid signature device is present and meaningful (not decorative slop)

## Subjective — human gate (never auto-passed)
- [ ] Taste sign-off on the signature "agent weaving a page" moment / overall feel

## §6b Spreadsheets section (added 2026-07-04; sibling of §6a Presentations)
- [x] Framed as a standalone capability ("tela makes spreadsheets"), not a per-page table trick — mirrors §6a
- [x] Visual is a REAL screenshot of the live grid (light register vs the dark page): formula bar `=SUM(B2:B6)`, currency + percent formats, over-budget cells flagged red, bold `=SUM` total row — show, don't describe
- [x] Engine not sold in headline/body/chrome; muted "Powered by defter" credit at the section foot (parallels Slidev/tahta on §6a)
- [x] Charts NOT claimed and NOT shown — tela's grid doesn't render sheet charts inline yet (honesty; user chose ship-as-is)
- [x] Deterministic gates green with the section live: tokens, axe WCAG 2.2 AA (4 viewports), reduced-motion, Lighthouse desktop + mobile
- [x] Verified dark + light, desktop (1440) + mobile (390); "sheets" woven into the pricing/self-host/terms/mcp enumerations

<!--
  Stop/SubagentStop hook keys off .cila/state.json `gate_required`. design-reviewer must cite a
  screenshot region + the relevant DESIGN.md/CONTENT.md clause before each sub-score.
-->
