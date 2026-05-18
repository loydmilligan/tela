# Spike: live-collab CRDT decision matrix

> Paper spike for the post-M6 "Live Collaboration" milestone. Decision-quality comparison so PO can pick an engine and lead can write the milestone brief.

## TL;DR

**Pick Yjs + y-prosemirror + y-indexeddb + Hocuspocus.** Only option that combines a ProseMirror-native binding under continuous maintenance, MIT licensing through the whole stack, mature IndexedDB offline + reconnect, named production deployments at our app shape (Linear, AFFiNE, Gitbook, JupyterLab, NextCloud), and a defensible bundle cost (~30 KB gzip vs current ~160 KB main-chunk baseline). Runner-up: a **hybrid prosemirror-collab + markdown-canonical** server-rebase OT design — lighter, simpler, no offline editing, and we own all the server logic ourselves.

Yjs-as-canonical-storage was rejected in M7 — this picks something different. Markdown stays canonical in `pages.body`; the Y.Doc is an ephemeral sync layer re-projected back to markdown on debounced save. That two-layer model is load-bearing for the rest of this doc.

## The constraints we're picking against

- **Markdown canonical in `pages.body TEXT`** — every option must round-trip to commonmark without lossy syntax additions.
- **Editor is Milkdown** (ProseMirror under the hood). Anything that doesn't have a ProseMirror binding pays full integration cost.
- **Post-M6 multi-user** — sessions exist, every endpoint is auth-gated, no anonymous edits.
- **License compatible with MIT/Apache** — no AGPL.
- **Modest scale** — v0 POC; single host; tens of users; hundreds of pages.
- **Must handle two tabs of one user AND two users on one page** (post-M6 single-user-multi-device + multi-user).

## Options

### 1. Yjs (+ y-prosemirror + y-indexeddb + y-websocket *or* Hocuspocus)

The default in the ProseMirror ecosystem. CRDT (YATA) under the hood, ~13 KB gzip core + ~10 KB y-prosemirror + ~5 KB y-indexeddb + ~5 KB y-websocket client.

- **Maturity:** v13 in production for ~6 years; v14 RC in March 2026. Heavy production use across our shape of app (wikis, docs, notebooks, comments).
- **Markdown round-trip:** y-prosemirror gives back a normal ProseMirror doc on demand; we serialize with `prosemirror-markdown` (which Milkdown already uses). Wikilinks survive because they're commonmark links `[T](tela://page/{id})` — no schema invention. Known sharp edge: overlapping marks in y-prosemirror have historical bugs (e.g. `<a>` around `<img>` dropped, issue #165). Won't hit us on the current feature set; revisit if the rich-view milestone picks features that nest marks.
- **Offline / reconnect:** y-indexeddb caches the Y.Doc; reconnect replays missing updates from server. This is the most battle-tested offline story of any option here.
- **Server:** y-websocket (the minimal reference server) or Hocuspocus (Tiptap's batteries-included server with a SQLite extension that fits our stack cleanly). Hocuspocus also gives us auth hooks, broadcast presence, and SQLite persistence — fewer moving parts to write.
- **Schema migration:** existing markdown docs get a one-shot Y.Doc initialization from parsed markdown on first collab session. Trivial.
- **License:** MIT all the way through (yjs core, y-prosemirror, y-websocket, y-indexeddb, Hocuspocus). The original AGPL flag in the task brief is a no-op — confirmed via the `yjs/y-websocket/LICENSE` and `ueberdosis/hocuspocus` license files.
- **Risks:** v13→v14 split mid-2026 — pin to v13 until v14 stabilizes. GC + delete-set storage growth at scale; document snapshot cadence belongs in the milestone brief, not now.

### 2. Automerge 3 (+ automerge-prosemirror)

MIT, Rust+WASM, Automerge 2.2 added rich text + a ProseMirror binding.

- **Maturity:** v3.2.6 (Apr 2026); recent 10× memory reduction. Production users skew research-shop (Ink & Switch). Fewer commercial-scale case studies than Yjs.
- **Markdown round-trip:** requires a `SchemaAdapter` between Automerge's block/marks model and ProseMirror's schema. Doable; binding docs are thinner than y-prosemirror's.
- **Offline:** yes. **Server:** roll your own — no Hocuspocus-equivalent.
- **Risks:** WASM blob ~hundreds of KB initial cost. Less ProseMirror history. v3 freshness.

### 3. Loro (+ loro-prosemirror)

MIT, Rust+WASM, Fugue CRDT — best raw benchmarks in the field. Core v1.23.x (Apr 2026); loro-prosemirror v0.4.3 (Feb 2026).

- **Maturity:** Loro 1.0 shipped 2025; ecosystem still small. loro-prosemirror has 145 stars vs y-prosemirror's deep adoption. Couldn't find a wiki-shape app running it.
- **Markdown round-trip:** binding → `prosemirror-markdown` — same shape as Yjs in principle, less community testing.
- **Offline:** loro-prosemirror doesn't ship an IndexedDB binding; we'd write one. **Server:** roll your own.
- **Risks:** breaking-change cycles between 0.x and 1.0 (HN flags this); small bus factor. Best CRDT perf isn't our binding constraint at v0 scale.

### 4. ProseMirror-collab (OT, server-authoritative)

Official MIT ProseMirror collab module from Marijn Haverbeke. ~3 KB; the framework's own answer to "collab without CRDT."

- **Maturity:** as mature as ProseMirror itself.
- **Markdown round-trip:** unchanged — same prosemirror-markdown as today. No schema gymnastics.
- **Offline:** **none.** Steps block when the authority is unreachable; long-gap reconnect may fail rebase. OK for "online editor"; not for "train wifi."
- **Server:** we write the authority — receive steps + version, apply, rebase-or-reject, broadcast. ~few hundred lines of Go.
- **Risks:** real dev cost (auth + step queue + version state + rebase + presence on top). No CRDT bloat in exchange.

### 5. Hybrid: prosemirror-collab + markdown canonical + side-channel presence

Option 4 explicitly built around our markdown-canonical constraint. Server keeps `pages.body` as truth, applies steps, serializes via prosemirror-markdown on debounced save, broadcasts accepted steps. Presence (cursors, typing) on a separate ephemeral channel so the auth path stays small.

- **Round-trip:** identical to today — markdown projection IS the canonical form, continuously.
- **Schema migration:** zero; no new doc format, no Y.Doc cache table.
- **Offline:** none.
- **Risks:** own bug surface; presence is on us.

## Comparison table

| Dimension | Yjs | Automerge 3 | Loro | PM-collab | Hybrid (PM-collab + md) |
|---|---|---|---|---|---|
| Years in prod | 6+ | 3+ (v3 new) | 1 (v1.0 fresh) | 10+ | n/a (we'd build) |
| Named production users (wiki shape) | Gitbook, AFFiNE, JupyterLab, NextCloud, Linear | Ink & Switch demos | none we could name | many ProseMirror apps | n/a |
| Bundle (initial JS gzip) | ~30 KB | ~hundreds KB (WASM) | ~hundreds KB (WASM) | ~3 KB | ~5 KB |
| Bundle (per-doc memory) | low | improved in v3 | low (Fugue) | trivial | trivial |
| Server cost | y-websocket trivial, Hocuspocus drop-in | roll your own | roll your own | roll your own auth + rebase | roll your own auth + rebase + md serialize |
| Markdown round-trip | ✅ via prosemirror-markdown | ✅ via SchemaAdapter + pm-markdown | ✅ via pm-markdown | ✅ native | ✅ native (canonical continuously) |
| Offline + reconnect | ✅ y-indexeddb | ✅ | ⚠️ no built-in IndexedDB binding | ❌ | ❌ |
| Schema migration | one-shot Y.Doc seed | adapter design | binding design | none | none |
| License (all parts) | MIT | MIT | MIT | MIT | MIT |
| Confidence we can debug it ourselves | medium-high (JS, big community) | medium (WASM internals) | low (WASM internals, small community) | high (small surface) | high (it's our code) |

## Recommendation

**Pick Yjs + y-prosemirror + y-indexeddb + Hocuspocus (SQLite extension).** Reasoning:

1. **It's the only option with mature offline.** y-indexeddb has been deployed by enough apps that the failure modes are understood. If we adopt OT and PO later asks "why doesn't editing on the train work," the answer "OT can't" is worse than "we shipped offline from day one."
2. **Bundle cost is small** (~30 KB gzip) against a current main-chunk baseline of 160 KB gzip — under 20% lift, and it goes into the milkdown chunk path anyway (already a separately-loaded chunk). Automerge and Loro both bring WASM blobs an order of magnitude larger.
3. **Hocuspocus + SQLite slots into our deploy unchanged.** Same compose-internal pattern as the backend service. We'd add one more internal service on the private network behind Caddy; no new host port.
4. **Markdown stays canonical.** The Y.Doc lives in a separate ephemeral cache table (`page_collab_state(page_id, ydoc_binary, last_snapshot_at)`); `pages.body` continues to be the source of truth for FTS5, backlinks, page history. On debounced save (e.g. 2s after edit-quiet), server serializes Y.Doc → prosemirror doc → markdown → writes `pages.body`. If `page_collab_state` is lost or corrupted, we rebuild from `pages.body` markdown — no data loss.
5. **Ecosystem trumps raw performance at v0 scale.** Loro is technically the fastest CRDT we benchmarked. None of those benchmark wins matter at hundreds of pages and tens of users. The community size matters when something breaks at 2am.

**Runner-up: hybrid prosemirror-collab + markdown** if PO decides offline editing is explicitly out of scope. It's lighter, has zero CRDT vocabulary to learn, and the server code is a fun ~500 LOC weekend. But we'd be committing to never solving the "I closed my laptop on the plane and lost edits" complaint without a rebuild.

**Do not pick:** Automerge (heavier, less ProseMirror history), Loro (too young for the wiki shape — revisit in 18 months).

## Open unknowns for PO

1. **Is offline editing in scope?** If no, the hybrid OT option becomes genuinely competitive — simpler, less weight, easier to debug. If yes, Yjs wins by default.
2. **Snapshot cadence and persistence layer.** Do we snapshot Y.Doc to disk every N seconds, or only on connection drain? Where does the snapshot live — separate table, SQLite blob column, S3-compat? This is a milestone-brief decision, not a spike decision, but PO should know we owe an answer.
3. **Presence scope.** Cursors and avatars only, or also "X is typing" and "X just selected this paragraph"? Yjs awareness solves this trivially; OT we'd build it. Cheap if planned in, expensive if bolted on.

## What I verified

- Yjs v14.0.0-rc.7 release date (Mar 2026) and production-users list from the official README.
- y-websocket and Hocuspocus license files — both MIT (the AGPL concern in the brief is a red herring).
- loro-prosemirror v0.4.3 release date (Feb 2026), star count (145), feature list (LoroUndoPlugin, EphemeralStore presence).
- Automerge v3.2.6 (Apr 2026), v3 memory improvements, automerge-prosemirror binding existence.
- prosemirror-collab is OT-flavoured ("OT without the stuff you don't need"), server-authoritative, rebase-on-reject.
- Hocuspocus has a built-in `@hocuspocus/extension-sqlite` for the storage layer.
- Notion uses a CRDT hybrid (CRDT for structure, LWW/OT-ish for in-block text) per their own engineers on HN — confirms that "CRDT for the whole thing" is not the only shape; supports the runner-up's viability.

## References

- Yjs core: https://github.com/yjs/yjs — MIT, v14 RC Mar 2026
- y-prosemirror: https://github.com/yjs/y-prosemirror — MIT
- y-indexeddb: docs at https://docs.yjs.dev/ecosystem/database-provider/y-indexeddb
- Hocuspocus: https://github.com/ueberdosis/hocuspocus — MIT, SQLite extension included
- Automerge: https://github.com/automerge/automerge — MIT, v3.2.6
- automerge-prosemirror: https://github.com/automerge/automerge-prosemirror
- Loro: https://github.com/loro-dev/loro — MIT, v1.x
- loro-prosemirror: https://github.com/loro-dev/loro-prosemirror — MIT, v0.4.3
- ProseMirror collab module: https://github.com/ProseMirror/prosemirror-collab — MIT
- ProseMirror collab guide: https://prosemirror.net/docs/guide/#collab
- Marijn Haverbeke on collab: https://marijnhaverbeke.nl/blog/collaborative-editing.html
- Loro vs Yjs discussion: https://discuss.yjs.dev/t/yjs-vs-loro-new-crdt-lib/2567
- Notion's collab approach (HN): https://news.ycombinator.com/item?id=37767739
