# tela landing

The standalone **marketing landing page** for tela — a separate Astro static site,
in the same repo as the app but built and deployed independently. `backend/` and
`frontend/` are untouched by this.

Stack: **Astro 6** · **Tailwind v4** (CSS-first `@theme`) · **OKLCH tokens** (one
`--brand-hue`) · self-hosted **Geist / Geist Mono** via the Astro Fonts API. No app
code, no app bundle.

## Contracts (locked, at repo root)

- `CONTENT.md` — what the page says (positioning, voice, the 13-section plan, final copy).
- `DESIGN.md` — the look ("Loom in the dark": refined dev-tool, dark-first indigo, woven-grid signature).
- `ACCEPTANCE.md` — the gate + visual criteria.

Tokens live in `src/styles/tokens.css` (the single source of truth). Never hardcode
hex / raw px / off-token color — the token-conformance gate fails the build if you do.

## Develop

```bash
make landing-dev      # or: cd landing && npm run dev   → http://localhost:4321
make landing-build    # static build → landing/dist/
make landing-gate     # a11y · token-conformance · reduced-motion · lighthouse(showcase)
```

The gates expect a running preview; they default to `BASE_URL=http://localhost:4321`.
(If port 4321 is taken, run `npm run preview -- --port <n>` and set `BASE_URL` to match.)

## Structure

- `src/pages/index.astro` — composes the page from section components.
- `src/components/` — `Hero`, `AgentWeave` (the signature "agent weaving a page"
  moment), `EditorMock` (faithful tela-editor mock, themeable + line-revealable),
  `Showcase`, `Compare`, `Pillars`, `ShowProduct`, `Search`, `SelfHost`,
  `Credibility`, `Faq`, `FinalCta`, plus `SiteHeader` / `SiteFooter` / `WovenBackdrop`.
- `src/layouts/Base.astro` — head, fonts, theme no-flash + toggle, JSON-LD, reveal IO.

The signature moment (AgentWeave) is the carved-out wow: an MCP `update_page`
tool-call types out while the loom threads sweep and the page materializes in the
editor. It is server-rendered as a resolved static frame first, then JS resets and
animates it on scroll — so no-JS / reduced-motion always show the finished state.

## Deploy

Static output (`landing/dist/`). The apex `https://tela.cagdas.io/` should serve this;
the app keeps `/login`, `/spaces`, `/share/*`, `/api/*`, etc. The Caddy route + a
`deploy-landing` target are **not wired yet** — see the deploy note in the repo root.
