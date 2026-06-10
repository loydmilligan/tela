# Live multiplayer Excalidraw

Real-time collaborative editing of an Excalidraw diagram embedded in a page —
multiple people drawing in the same diagram at once, with live cursors, and the
result saved durably to `pages.body` without a manual Save.

This builds on the existing live-collab stack (`docs/architecture.md` → LiveCollab)
rather than introducing a parallel one. The whole feature is an **ephemeral
transport layer** beside the persisted Yjs doc; it leaves the persisted data
model untouched.

## The five invariants (load-bearing — preserve these)

1. **The persisted model never changes.** A diagram is still JSON in the
   ```` ```excalidraw ```` fence inside `pages.body` (the atom's `sceneJSON`).
   So the view renderer, export, WebDAV sync, zip, search, and import stay
   collab-unaware. This is the main reason the feature adds ~no drift.
2. **The live channel is physically separate from the persisted Yjs doc.** It
   rides a relay-only WS tag (`0x07`) that is **never persisted** — mirroring
   awareness. Locked by `TestIntegration_WSPage_DiagramRebroadcastsAndNeverPersists`.
3. **One writeback path.** PNG upload + atom write + body persist happen in a
   single `commitScene` used by the explicit Save, idle checkpoints, and
   close-commit. (This folded in the old `db43d88` force-save.)
4. **All collab code lives in `src/lib/collab/*`** (frontend hard-rule #6). The
   edit sheet imports only *types* from there; it pulls in no Yjs.
5. **Convergence is Excalidraw's own `reconcileElements`** (per-element
   last-writer-wins by `version`). We write no merge logic.

## Architecture

```
 Peer A canvas ──onChange──▶ DiagramSession ──tag 0x07──▶ relay ──▶ DiagramSession ──reconcile──▶ Peer B canvas
       ▲                         (transport only)        (no DB)                                      │
       └───────────────────────────── reconcileElements ◀───────────────────────────────────────────┘

 Persistence (orthogonal):  canvas ──idle/close──▶ commitScene ──PNG+setNodeMarkup──▶ atom (Yjs prose doc) ──leader──▶ pages.body
```

### Pieces

- **Backend** `backend/internal/api/pages_ws.go` — tag `0x07` (`tagDiagram`):
  pure fan-out to other peers in the page room, never appended to
  `page_yjs_updates`. Identical handling to awareness.
- **Provider** `src/lib/collab/tela-provider.ts` — `sendEphemeral()` /
  `onEphemeral()`: a raw channel sharing the page ws but **not** the Y.Doc.
  Tag constant in `src/lib/collab/encode.ts` (`TAG_EPHEMERAL`).
- **`src/lib/collab/diagram-session.ts`** — the one new piece of real logic.
  Per-open-diagram session: diffs/broadcasts scene deltas + pointers, dispatches
  remote frames, tracks collaborators, late-join catchup, checkpoint leadership.
  Transport only — knows nothing about Excalidraw's runtime. Unit-tested
  (`diagram-session.test.ts`, `npm run test:unit`).
- **Edit sheet** `src/components/app/excalidraw-edit-sheet.tsx` — when a session
  is present, streams `onChange` into it, applies remote deltas via
  `reconcileElements` + `updateScene`, renders collaborators, and owns the
  writeback controller (`commitScene` + checkpoint + close-commit).
- **Editor** `src/components/app/milkdown-editor.tsx` (collab branch) — builds
  the `DiagramSession` while the sheet is open, and the `onSave` wrapper that
  persists (atom write + force body save).
- **Atom + markdown** `milkdown-excalidraw.ts` + `lib/markdown/transforms/excalidraw.ts`
  — carry the stable `diagramId` (fence key `diagram_id`).

## Wire protocol (tag 0x07 payload)

JSON over `TextEncoder`. Every message has `k` (room key = diagram id) and `u`
(`{id, username, colorIdx, clientId}`):

| `t` | meaning | fields |
|-----|---------|--------|
| `s` | scene delta (or full, in answer to `r`) | `e`: changed elements |
| `p` | pointer | `x`, `y` |
| `r` | state request (late-join) | — |
| `l` | leave (close) | — |

A peer ignores frames whose `k` ≠ its room key. The server only fans out to
*other* peers, so there's no self-echo to filter.

> Wire format is deliberately JSON, not binary — the throttled delta stream is
> small and the two-browser test confirmed it feels live. Revisit only if a
> payload-size problem actually shows up.

## Room key: the stable `diagramId`

The room key is a stable per-diagram id (`crypto.randomUUID()`), stamped at
insertion and stored as `diagram_id` in the fence. It is **not** the
`scene_hash` — the hash changes on every save/checkpoint, which would desync a
late joiner mid-session. Legacy diagrams (no id) fall back to `scene_hash` as
the key and get an id stamped on the next save.

The id also makes saves robust to the atom **drifting position** when
collaborators edit the prose around it during a long session: `onSave` locates
the atom by `diagramId` (`findExcalidrawPos`), not the captured position.

## Writeback model

There is no "Save moment" in a live session — the scene is persisted by:

- **Idle checkpoint** — after `CHECKPOINT_IDLE_MS` (2.5s) without an edit (local
  or remote), the **checkpoint leader** (lowest `clientId` among active peers —
  keeps N peers from all uploading the same PNG) commits the converged scene
  silently. A crash loses at most this window.
- **Close-commit** — closing the sheet in a session commits a final time
  (there's no "discard" in collab — your edits are already shared). Solo mode
  keeps classic discard-on-cancel.
- **Explicit Save** — unchanged, always available.

All three route through `commitScene`, which dedupes against the last persisted
scene+altText so redundant checkpoints/closes are cheap. The PNG upload is
content-addressed, so even concurrent commits of a converged scene are idempotent.

## Boundary cases (the robustness surface)

| Case | Handling |
|------|----------|
| 2nd editor opens an active diagram | seeds from committed scene, then `r` request → present peers reply with full scene |
| concurrent draw | per-element LWW via `reconcileElements` |
| one leaves, others stay | `l` + awareness removal drop its cursor; others continue |
| last editor leaves | close-commit persists final scene |
| crash before commit | last idle checkpoint is the floor (≤2.5s lost) |
| open while a commit is in flight | seeds from committed scene; `commitScene` is reentrancy-guarded |
| reader (not in canvas) | sees last-written PNG; refreshes on next checkpoint |

## Deferred / future

- **Binary wire format** — only if payload size becomes a problem.
- **Checkpoint-leadership churn** — current election is best-effort (idempotent
  anyway); a tighter handoff isn't needed at small scale.
- **Remote-edit re-broadcast echo** — applying a remote delta re-fires
  `onChange` → one extra bounded broadcast (matches excalidraw-app; reconcile
  makes it harmless). Optimize only if peer counts grow.
- **Phase 3 polish** — explicit "X is editing" presence; and the user-facing
  tela Docs space (space 16) entry, due when this ships (per CLAUDE.md).

## Tests / gates

- Backend: `TestIntegration_WSPage_DiagramRebroadcastsAndNeverPersists`
  (relay + the never-persisted tripwire) — part of `make test`.
- Frontend: `src/lib/collab/diagram-session.test.ts` — `npm run test:unit`.
