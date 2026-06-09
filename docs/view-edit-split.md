# View / Edit split (Confluence-style) — design contract

Status: **in progress** (design locked; core shipped). Owner: see git blame.

### Implementation status

- ✅ **Parse spine** — shared remark stack (`lib/markdown/remark-stack.ts`) +
  Milkdown-free transforms (`lib/markdown/transforms/*`) the editor and view
  both import. No second parser.
- ✅ **`MarkdownView`** (`components/view/MarkdownView.tsx`) — renders headings,
  lists/tasks, quote, code (refractor), tables, callouts, highlight, math
  (KaTeX), mermaid + chart (shared `lib/diagrams/*` cores), excalidraw (server
  PNG), wikilinks (resolver-aware), and tabs (interactive). Validated in
  Storybook + the live app.
- ✅ **View-first `PageView`** — read view by default; `PageEditor` (collab)
  mounts only on `?edit=1` / draft. Edit ⇄ Done toggle. Verified in-app: view
  loads no editor chunk, no `/yjs`, no websocket.
- ✅ **Anti-drift gate** — `scripts/blocks-manifest.mjs` requires every block to
  declare a view-render status (VIEW_RENDERED / VIEW_DEGRADES).
- ⏳ **Pending** — comments in view (read+reply, phase 4); dedicated renderers
  for the degrading directive blocks (pull-quote, embed, file, kanban,
  stat-grid, timeline, calendar) + collapsible; swapping `/read`, `/public`,
  `/share` to `MarkdownView`; the runtime render-parity test (the gate is
  classification-only for now — FE has no test infra yet).

## Why

Today every page open boots the full collaborative Milkdown editor — even to *read*. That couples reading to editing and causes the two costs we measured on prod:

- **~440 ms one-time editor first-mount** per session (building 70+ plugins, ProseMirror schema, refractor, CSS) — and it's on-screen render work, so it can't be pre-warmed off-screen (we tried; see git history of `EditorWarmup`).
- **~180 ms body-blank on every page open** in the app, because collab binds the doc to an empty Yjs doc and waits for the `/api/pages/{id}/yjs` round-trip (uncached, re-fetched on every back-and-forth).

It also means a reader is one stray keystroke from editing.

The root fix is to **not load the editor to read**. Reading renders markdown → UI directly (no ProseMirror, no Yjs); editing is an explicit mode that loads the editor.

## Product decisions (locked)

1. **View-first for everyone.** Opening any page shows a read-only view. An explicit **Edit** enters the editor. Editors/owners see the Edit affordance; viewers never do.
2. **In-place toggle, same URL.** `/spaces/{id}/pages/{id}` stays one URL; Edit flips the page into the editor in place (no separate `/edit` route). A `?edit=1` search param syncs the mode so a refresh-while-editing stays in edit; cleared on exit. (This is state on one route, *not* a second route.)
3. **Comments read + reply in view.** View shows comment highlights + the thread panel; read and reply work without entering edit. Creating a *new* comment from a selection in view needs a small non-PM selection-capture — a follow-up, not phase 1.

## The drift problem, and how we kill it "all the way"

A naive view renderer is a *second* implementation of every block that drifts from the editor. We avoid that with **one canonical content model, one parse source, shared presentation, and an automated parity guarantee** — five layers, in order of strength:

1. **One content model — already true.** `pages.body` is canonical markdown (no block table). View and edit read the same bytes. No second data model, ever.

2. **One parse source.** The 9 custom remark plugins (`calloutsRemarkPlugin`, `directiveRemarkPlugin`, `mathRemarkPlugin`, `highlightRemarkPlugin`, `collapsiblesRemarkPlugin`, `excalidrawRemarkPlugin`, `wikilinkBracketRemarkPlugin`, + commonmark/gfm) are today wired inline in the Milkdown builder (`milkdown-editor.tsx:666–805`). Extract that list into **one shared module** (`lib/markdown/remark-stack.ts`) that *both* the editor builder and the standalone view parser import. The `unified`/`remark-parse`/`remark-gfm`/`remark-math`/`remark-directive`/`mdast-util-*` stack is **already installed** (transitively via `@milkdown/kit`), so the view parser is `unified().use(sharedStack).parse(body)` → mdast. Same plugins in, same tree out.

3. **One text projection.** Comment anchoring is pure text-fingerprint over `doc.textBetween(0, size, '\n')` (`lib/comments/anchor.ts`, `anchor-decoration.ts`) — *not* PM positions. For view-mode comments to land on the same ranges as edit-mode, **both modes must project text identically.** We define the canonical projection over **mdast** (`lib/markdown/text-projection.ts`) and make the view comment layer use it; the editor's projection must agree with it (parity-gated, below). This is the subtle invariant — get it wrong and anchors desync silently.

4. **Shared presentational components.** One React component per block's *visual* (`components/blocks/Callout.tsx`, `Tabs.tsx`, `Kanban.tsx`, …). The **view** renderer maps mdast → these directly. The **editor** renders the same components through `@prosemirror-adapter/react`'s node-view factory (already wired via `ProsemirrorAdapterProvider`), wrapping them with the editable contentDOM / selection chrome. Where an editable content-hole makes a full React node-view impractical (kanban cards, tab panels, callout body), the editor keeps a thinner node-view but renders the *same chrome component* — so the look has one source even when the editing plumbing differs.

5. **Automated parity guarantee (the backstop).** Code-sharing reduces drift; the gate *detects* it regardless:
   - **Manifest gate:** extend `scripts/blocks-manifest.mjs` with a `VIEW_BLOCKS` registry — every authorable block must map to a view-renderer component, mirroring the existing `PLUGIN_BLOCKS` check. A new block can't ship rendering in edit but not view (build fails). Runs in `make blocks-gate` (already part of `make test`).
   - **Render-parity test:** a corpus of markdown fixtures rendered through (a) the editor in read-only → normalized DOM, and (b) `MarkdownView` → normalized DOM; assert structural + **text-projection** equivalence. This catches both visual drift and the anchoring-projection invariant. (FE has no jsdom/vitest today — this gate ships as a node script like `blocks-manifest.mjs`, using `react-dom/server` for the view side; the edit side compares against the shared components' output. Phased: manifest check first, full cross-parity once the renderer exists.)

## Architecture

```
pages.body (markdown, canonical)
        │
        ├─ VIEW: lib/markdown/parse.ts ──(shared remark stack)──► mdast
        │        components/view/MarkdownView.tsx  (recursive mdast → React)
        │            └─ components/blocks/*  (shared presentational components)
        │        → instant, no ProseMirror, no Yjs, no /yjs fetch
        │
        └─ EDIT: milkdown-editor.tsx (same remark stack) ──► ProseMirror
                 node-views render the SAME components/blocks/* via prosemirror-adapter
                 collab (Y.Doc + TelaProvider) created on enter-edit, destroyed on exit
```

- **View** ships none of: Yjs, slash/bubble/block-handle, emoji picker/autocomplete, image-upload, mira-paste, url-unfurl, `@excalidraw/excalidraw` (view shows the server PNG at `/api/diagrams/{pageId}/{sceneHash}.png`). It lazy-loads only what a given doc needs (katex, mermaid, echarts for math/diagram/chart blocks present).
- **Edit** is unchanged in capability; it just stops being the read path.

## Mode model (PageView)

`PageView` becomes mode-aware (`view` default):

- `view`: render `<MarkdownView body={page.body} … />`. No collab, no editor chunk. Body paints from the cached `usePage` data immediately → kills the ~180 ms blank.
- `edit`: render the existing collab `MilkdownEditor` (`collabPageId=page.id`). Entered via the Edit button (gated on editor/owner role — reuse `isViewer`/`roleResolved`, `PageView.tsx:333–351`). The Y.Doc/provider create-on-enter, destroy-on-exit (drive the existing lifecycle at `milkdown-editor.tsx:367–384` / `1014–1021` by mode instead of mount).
- `?edit=1` syncs mode across refresh; Edit/Done toggles it. Back-button returns to view.
- The existing **"Read mode"** menu item (navigates to `/read`) becomes redundant for the app and can be retired once view is the default.

### Read surfaces fold in (bonus)

`/read`, `/public/*`, `/share/*` already render read-only Milkdown via `ReaderShell`. Swap them to `MarkdownView` too: they get the same instant render, and **public/share stop shipping the editor entirely** (large chunk win for the open-web surfaces). Once nothing read-only uses Milkdown, **delete the `readOnly` + `wikilinkMode==='share'` branches from `milkdown-editor.tsx`** — the editor becomes edit-only, a real debt paydown.

## What this supersedes

- The proposed `/yjs` snapshot cache patch is **dropped** — collab only runs in edit (an explicit action where a short load is acceptable), so the back-and-forth blank disappears at the root.
- The hover-prefetch of the editor chunk is **repurposed**: prefetch on **Edit-button hover/intent** instead of page-link hover.
- `EditorWarmup` stays removed.

## Migration phases (each shippable)

0. **Shared parse layer.** Extract `remark-stack.ts`; build `lib/markdown/parse.ts`. Editor builder imports the shared stack (behavior-neutral refactor). Pin/test base-markdown parity (Milkdown preset vs standalone) against a fixture corpus.
1. **`MarkdownView` + easy blocks** (headings, lists, quote, code+prism, table, image, divider, callout, pull-quote, embed, file, timeline, math, highlight, footnote, wikilink) + Storybook + golden snapshots.
2. **Heavy/lazy display blocks**: mermaid (→SVG), chart (echarts), excalidraw (→PNG, no lib), stat-grid, calendar — each lazy.
3. **Interactive-in-view blocks**: collapsible, tabs, kanban (static), table sort/filter.
4. **Comments in view** (read + reply): shared mdast text-projection + `resolveAnchor`; unify edit-side projection; parity-gate it.
5. **Mode toggle in PageView** (view default, Edit button, collab on enter/exit, `?edit` sync). Swap `/read`, `/public`, `/share` to `MarkdownView`.
6. **Cleanup + gate**: remove the editor's `readOnly`/share branches; extend `blocks-manifest.mjs` with the `VIEW_BLOCKS` requirement + render-parity test. Update repo `docs/` and the **tela Docs space (16)** — the read/edit UX is user-visible.

## Risks / open items

- **Editable-content blocks** (kanban, tabs, callout body) — node-view sharing is partial; mitigated by shared chrome + parity gate.
- **Text-projection unification** is the sharp edge: edit and view must project identical text or comment anchors desync. Gated, but needs care in phase 4.
- **Base-markdown parity** between Milkdown's presets and the standalone parser — pin versions, corpus-test in phase 0.
- **New-comment-from-selection in view** (no PM) — follow-up after phase 4.
- FE has **no test infra** (CLAUDE.md) — parity/snapshot gates ship as node scripts, not vitest.
