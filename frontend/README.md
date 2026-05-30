# tela — frontend

React 19 + TypeScript + Vite SPA for [tela](../README.md). Tailwind v4 + Radix + Milkdown editor + TanStack Query/Router + Orama + cmdk + Lucide + Storybook.

## Dev

```bash
npm install
npm run dev          # Vite dev server on :5173 (proxies /api → backend :8080)
npm run storybook    # component dev surface on :6006
npm run build        # production build
npm run lint         # eslint
```

From the repo root, `make fe-dev` / `make storybook` wrap these, and `make dev` runs the frontend alongside the backend. The dev server proxies `/api` to the backend on `:8080` (see `vite.config.ts`); start the backend with `make be-dev`.

## Conventions (enforced — see [`../CLAUDE.md`](../CLAUDE.md))

- Design tokens in `src/styles/tokens.css`, semantic names only — never hardcode hex / raw px / radii.
- Theming via CSS custom properties on `[data-theme="..."]`; `@layer tokens, base, components, utilities` ordering is locked.
- Owned Radix + shadcn-style primitives in `src/components/ui/` only — no MUI/Chakra/Mantine/Ant/daisyUI. Every new UI element uses an owned primitive (build it with a Storybook story first if missing).
- Yjs may be imported only in `src/lib/collab/*` and the collab branch of `milkdown-editor.tsx`.
- State is TanStack Query; routing is TanStack Router (the command palette is a `RouterProvider` sibling — use `router.navigate()`).

## Layout

- `src/components/{ui,app}` — primitives + composed components.
- `src/lib` — `collab/` (Yjs transport), `comments/`, `queries/` (TanStack hooks), `milkdown-*` plugins + `milkdown-editor.tsx`.
- `src/routes` — TanStack Router routes.
- `src/styles` — tokens + theme layers.

## Tests

There is no unit-test harness yet (no jsdom / vitest config). Storybook is the current component verification surface.
