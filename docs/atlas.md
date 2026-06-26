# atlas — source-grounded, coverage-audited doc generation (inside tela)

> Status: **Phases 1–6 (backend) done; UI first cut done — all on `feat/atlas`.** Ported engine (`EngineStore` Postgres, chat→`TELA_LLM_URL`, embed→`rag.Embedder` via `EmbedFunc`), in-process publisher, executor + SSE + `ResumeDangling`, HTTP API (gating tested), **delta runs + freshness scheduler + drift** (P4), **per-space secret store + private-git & Jira sources** (P5), **MCP tools** `atlas_list_sources`/`atlas_run`/`atlas_sync`/`atlas_run_status` (P6), **run-finish notifications** via tela's native in-app system (P6). UI: the generation console (`/spaces/$spaceId/atlas`), `StatusBadge` + `CoverageGauge` primitives, a space-header entry point — **visually QA'd**. `cmd/atlasdiff` is the 1-1 fidelity harness; the live compass 1-1 **passed the deterministic gate** (64 files / 77 surface / 433 chunks + exact spine items). **Remaining:** the **tela Docs space (16) end-user docs** (at deploy) and opening the PR off `feat/atlas`. Baseline for reference: compass `fa5b543` → Atlas space 51; models `qwen3:30b-a3b-instruct-2507-q4_K_M` + `qwen3-embedding:0.6b` (compass `fa5b543` → Atlas space 51 reference; models `qwen3:30b-a3b-instruct-2507-q4_K_M` + `qwen3-embedding:0.6b`; baseline 64 files / 77 surface / 433 chunks / 13 pages / 100% must-cover). This is the developer/architecture contract for bringing the standalone `atlas` system natively into tela. It is the source of truth for the build; update it as phases land. End-user docs go to the tela Docs space (space 16) per `CLAUDE.md`.

## What this is

`atlas` (the experimental project at `~/proj/atlas`) turns **source systems** (git repos, Jira projects) into **coverage-audited, grounded documentation**: it runs a deterministic + LLM pipeline that produces a wiki of cited pages, and measures those pages against an objective **spine** of the source's real surface (routes, env vars, CLI flags, DB models, …) — reporting *what fraction of the important surface is actually documented*. That coverage number is the differentiator a free-roaming "ask the repo" tool can't produce, because it has no ground-truth inventory.

atlas already **publishes into tela** today over a REST client. Bringing it *inside* tela collapses the generator and its destination into one system: a tela space becomes both the output and (via tela's existing RAG/`research`/`/ask`) the answer surface. Coverage *guarantees* the askable corpus is complete.

The full description of standalone atlas is `~/proj/atlas/atlasdesc.md` (read it for the domain model and pipeline detail).

## The native model — "the space *is* the project"

Standalone atlas's `Project` only existed to group sources + an LLM connection + an output dir + a freshness cadence. Inside tela the connection is gone (instance LLM), the output *is* a space, and cadence lives on the space — so **the managed space is the project**. No `projects` table.

```
Space (atlas-managed)          ← unit of management; freshness/cadence live here
  └─ Source (git | jira)       ← 1+ per space; each optionally under a top-dir parent page
       └─ Run (full | delta)   ← one generation pass; live progress; produces Coverage
            ├─ Pages           ← real tela pages, run-owned (props.generator=atlas)
            └─ Coverage        ← native dashboard: must-cover %, gap list, drift
```

**"Managed" is the one new distinction.** A space becomes atlas-managed when a source is bound to it (`atlas_sources.space_id`). One source per space = "completely atlas-managed" (the primary case); multiple sources nest under per-source parent pages (atlas's current top-dir shape). Managed spaces are surfaced distinctly in the UI and their generated subtree is machine-maintained.

## Access & ownership (reuse tela's model — no new permission system)

The managed space *is* the project, and a tela space is already either **personal** (a user owns) or **org-owned** (`spaces.org_id` set), with access resolved through the `space_access` view to one effective role (`owner > editor > viewer`; org/group grants folded in). So "org or personal projects, org ones managed by org admins" falls out for free:

- **Manage atlas** (add/edit/delete sources, trigger runs, set cadence, unbind): `requireSpaceManage(spaceID)` = effective space role `owner` **OR** (`space.org_id != null` AND `requireOrgAdmin(org_id)`). Instance admins pass as virtual org admins. *Management is admin-level on purpose — a run fetches an external source, spends LLM budget, and rewrites the whole generated subtree.*
- **View** coverage / runs / sources: `requireMembership` (viewer+), like any space resource; the api-key space-scope ceiling applies automatically.
- Creating an org-owned managed space / binding a source uses existing org-space creation rights (`createSpaceCore` already grants owner + org-editor).
- This also fixes the publish-identity question: authority is checked when a **user triggers/configures** a run; the publish **writes** run in the background via direct SQL (the summarize/agreement pattern — no per-write identity, no revision/notification pollution), then queue RAG reindex.

## Decisions (locked with Cagdas)

1. **Space mapping** — a source manages a space (or a top-dir within it). Whole managed spaces are tracked distinctly; no separate `projects` concept. Don't over-engineer the marker.
2. **Models** — **reuse tela's instance LLM + embedder** (same Ollama box, same chat model, same `qwen3-embedding:0.6b`). No per-project model-config UI, no `connections` table. (See *Fidelity* for how this is reconciled with the lifted `llm` client.)
3. **Human-edit contract on generated pages** — *open*; settled at the publish stage (Phase 2). Default to revisit: atlas's model (generated subtree is machine-maintained, re-run clobbers body wholesale, tela revisions recover). "Detach on edit" can layer on later with no schema churn.
4. **Scope** — build all of it; sequencing below.

## Fidelity is the hard requirement

The ingestion/generation pipeline is calibrated (chunk sizes, retrieval fusion weights, prompt wording, temperatures, K/context budgets, repair thresholds, mermaid rules). **No regression.** The existing atlas-generated spaces are the golden baseline for a 1-1 diff.

Strategy: **lift the atlas Go packages verbatim and rewire only the seams.** Re-deriving any stage from a summary is forbidden.

| atlas package | disposition inside tela |
|---|---|
| `internal/core` | **lift verbatim** → `backend/internal/atlas/core` (pure types, no deps) |
| `internal/source` (git, jira, citeurl) | **lift verbatim** → `backend/internal/atlas/source` (pure logic + `git`/REST exec) |
| `internal/llm` | **lift verbatim** → `backend/internal/atlas/llm`. **Chat:** atlas's client pointed at `TELA_LLM_URL` (already `/v1`) — identical `/chat/completions` transport, so the calibrated retry/backoff/concurrency/temperatures are preserved against tela's instance model. **Embed:** tela's embed endpoint is **Ollama-native `/api/embed`**, not OpenAI `/embeddings`, so the lifted client's embed path can't hit it directly. Instead a small optional `EmbedFunc` seam on the client **delegates embedding to tela's `rag.Embedder`** (same instance Ollama, same `qwen3-embedding:0.6b`). Vectors are per-text identical regardless of batching, so there is **no retrieval regression** — and generation now embeds through the exact embedder tela's own RAG uses (one model, one endpoint, shared metering). nil `EmbedFunc` keeps the original HTTP path for standalone use. |
| `internal/engine` incl. `retrieve.go` | **lift verbatim**; only `RunContext.Store` changes type (see Store seam). `retrieve.go` (in-memory dense+BM25 fusion, `denseWeight=0.6`) is **kept as-is** — swapping to tela's pgvector RRF would change retrieval ranking → change drafting inputs → diverge from the reference. pgvector is used only to *persist* chunk vectors for resume/delta; the in-memory retriever is rebuilt per run exactly as atlas does. |
| `internal/store` (SQLite) | **not lifted** — replaced by a narrow `EngineStore` interface backed by tela Postgres. A parallel SQLite layer would be the tech debt we're avoiding. |
| `engine/deliver.go` + `internal/tela` REST client | **rewritten in-process** — publish calls tela's `createPageCore`/`updatePageCore`; the `internal/tela` import vanishes; `destinations`/`page_deliveries` collapse to space-ownership + `props.generator=atlas`. |
| `internal/notify`, `internal/secret` | **pluggable / deferred** (Phase 6 / Phase 5). `notify` is a no-op hook initially; `secret` (git/jira creds) is a new minimal store. |

### The Store seam (the only forced change in the engine)

The engine calls exactly 17 methods on `rc.Store`. Converting `RunContext.Store *store.Store` → `Store EngineStore` (interface) is the single edit the lifted stages need. The pure-pipeline subset (everything except the connection/destination calls, which live in the rewritten publish path):

```
AppendEvent · UpdateRun · GetRun · SetSourceRef
SaveFiles · SaveSpine · SaveChunks · SaveVectors
CopyChunksToRun · RunChunksWithVectors            (delta reuse / resume)
SavePages · UpdatePageBody
SaveRunCoverage · SaveRunStats
```

Connection/destination-shaped calls (`GetConnection`, `ResolveModel`, `ListDestinations`) are removed: the LLM `ModelCfg` is injected from tela env at run construction, and the publish target is the bound space.

## Data model (new migrations, forward-only `NNNN_atlas_*.sql`)

Run-scoped ingestion tables mirror atlas's (disposable, `ON DELETE CASCADE` from runs) but in Postgres with `pgvector`:

- `atlas_sources` — `id, space_id (FK), parent_page_id (nullable), type ('git'|'jira'), location, name, ref, branch, subpath, include, exclude, secret_id (nullable, Phase 5), cadence, auto_update, last_refresh_at, created_at`. Binding a row makes the space atlas-managed.
- `atlas_runs` — `id, source_id (FK), kind ('full'|'delta'), baseline_id, changeset_json, status, stage, err, coverage_json, stats_json, started_at, finished_at`.
- `atlas_run_events` — `id, run_id (FK), stage, level, msg, cur, total, at` (durable SSE replay).
- `atlas_files` — per-run inventory (`path, lang, size, lines, hash`).
- `atlas_symbols` — the spine (`kind, name, file, line, detail`).
- `atlas_chunks` — `run_id, file, start_line, end_line, kind, symbol, text, embedding vector(1024), content_tsv`. Run-scoped; the in-memory retriever is rebuilt from these rows.
- Managed-space marker — derived from `atlas_sources` (lightweight denorm on `spaces` only if a hot path needs it; not over-engineered).

Generated **pages** are ordinary `pages` rows in the bound space, tagged `props.generator=atlas` (+ source/commit/`provenance=agent`/`generated_at`), created/updated via the existing page core funcs (so revisions, link-sync, and RAG reindex all fire automatically).

## Backend layout

```
backend/internal/atlas/            # the lifted engine — no internal/api deps
  core/ · source/{git,jira} · llm/ · engine/ (stages incl. retrieve.go) · coverage seam
backend/internal/api/atlas_*.go    # handlers, run executor, scheduler, SSE, MCP tools, publish-in-process
backend/internal/db/migrations/    # NNNN_atlas_*.sql
```

Generation embeds in bulk against the shared warm instance Ollama; the executor gets a bounded embed-concurrency budget (the lifted `llm` client's gate) so a big repo ingest doesn't starve live page reindexing — the one real ops nuance.

## Run lifecycle / progress

Persisted runs; an executor goroutine pool drives `engine.Default().Run()`; `ResumeDangling` on boot continues runs left `running` (lifted from atlas `resume.go`). Live progress streams over SSE (`/api/atlas/runs/{id}/stream`) replayed from `atlas_run_events`, mirroring tela's `/ask` stream pattern; the FE hook mirrors `useAskDocsStream`.

## UI (Phase 3) — space-centric, native

The managed space view *is* the project detail: **Sources / Runs / Coverage** alongside its pages, live run progress, and a coverage dashboard. Strictly tela tokens + owned primitives (the dark "Instrument Panel" aesthetic does **not** carry over). Two genuinely new owned primitives, each with a Storybook story: a **radial coverage gauge** and a **live run/log stream**.

## 1-1 comparison harness

After Phase 2, generate into a fresh tela space from the same source + same models as a deployed atlas reference space, then diff: spine items (count + kinds), page set (titles/slugs/order), coverage numbers (must-rate, gaps, citations, mermaid), and body content. Divergence = a porting bug. (Build a small diff script under `scripts/` or a Go test fixture.)

## Sequencing

1. **Foundations** — migrations + `internal/atlas` skeleton (verbatim copy + import rewrite) + `EngineStore` interface + managed-space marker.
2. **Core loop (public git, full run)** — spine→…→repair→publish-in-process; executor + persistence + resume; minimal trigger/read API; 1-1 diff vs reference.
3. **Native operator UI** — managed-space Sources/Runs/Coverage + live progress + sidebar distinction.
4. **Delta + freshness** — change-gated delta re-ingest, cadence scheduler, publish-prune, drift.
5. **Jira + secret store** — port `source/jira`; minimal write-only git/jira secret store (unblocks private repos too).
6. **Notifications + MCP tools + docs** — run-finish notifications (reuse mailer), MCP tools (trigger run / read coverage), repo + space-16 docs.
