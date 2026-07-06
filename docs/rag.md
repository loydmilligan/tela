# RAG — semantic retrieval

> The product features built ON this engine — related pages, link suggestions,
> overlap detection, knowledge gaps, and the vision — live in
> [`knowledge-intelligence.md`](knowledge-intelligence.md). This doc is the engine.

How tela turns `pages.body` into searchable meaning, how that index stays
healthy on its own, and how to measure and evolve it. The keystroke-path search
UX (Orama + Postgres FTS) lives in [`search.md`](search.md); this doc is the
embedding/RAG half (`backend/internal/rag`).

## Summary

tela's Ask ("talk to your docs") is a two-halves system over one index. **Ingest**
turns every page and attachment into embedded, retrievable chunks the moment it's
saved (self-healing, background). **Retrieve → answer** takes a question, pulls the
right chunks with hybrid search + optional cross-encoder rerank, expands the pages
that matter to their full body, and has an LLM answer *only* from that grounding
with cited sources — streamed, and resumable across a dropped connection. Every
retrieval is authorized in-query through the live page row, so a chunk can never
out-scope its page.

The rest of this doc walks both halves; the two schemas below are the map.

### Schema A — how docs become RAG-ready (ingest / index-time)

Runs in the background a few seconds after every save (`autoreindex.go`), per page
**and** per attachment. The whole path is a disposable cache — fully rebuildable
from `pages` + `space_files`.

```
 page saved / file uploaded
        │
        ▼
 ┌──────────────────────────────────────────────┐
 │ 1. NORMALIZE source text                      │
 │    • page body: strip Excalidraw fences       │
 │    • sheet page: project Defter → prose        │  sheetproj.Project
 │      (materialize cell/formula values)         │
 │    • attachment: extract text (PDF/md/txt/     │  index_file.go
 │      csv/json); non-text bytes skipped         │
 └──────────────────────────────────────────────┘
        │
        ▼
 ┌──────────────────────────────────────────────┐
 │ 2. CHUNK  (heading-aware, ~1700-char target)  │  chunk.go
 │    each chunk = section under a heading path   │
 └──────────────────────────────────────────────┘
        │
        ▼
 ┌──────────────────────────────────────────────┐
 │ 3. CONTEXTUALIZE  → EmbedText                  │
 │    fold page title + heading breadcrumb into   │
 │    each chunk so it's self-contained           │
 └──────────────────────────────────────────────┘
        │
        ▼
 ┌──────────────────────────────────────────────┐
 │ 4. EMBED  (1024-d vector)                      │  embed.go / embed_openai.go
 │    cache key = hash(model + EmbedText):        │
 │    unchanged chunk reuses its stored vector     │
 └──────────────────────────────────────────────┘
        │
        ▼
 ┌──────────────────────────────────────────────┐
 │ 5. STORE  page_chunks / file_chunks            │
 │    content + content_tsv (FTS) + embedding +    │
 │    embed_model stamp                            │
 └──────────────────────────────────────────────┘

 ── parallel enrichment track (summarize/, not on the retrieval path) ──
 page saved → debounced LLM summary → pages.props.summary
   feeds blog excerpts, public/SEO meta descriptions, the title hover hint,
   and the "summary out of date" freshness dot — NOT the Ask grounding.
```

Robustness (steps 1–5): a save burst debounces to one reindex; a failed reindex
retries with backoff (30s → 10m); an independent stale sweep re-queues anything
missing/out-of-date after an outage or restart. See *Robustness model* below.

### Schema B — how a question is answered (query-time)

```
 question
    │
    ▼
 ┌───────────────────────────────────────────────┐
 │ 1. RETRIEVE  (hybrid, access-scoped)           │  search.go
 │    • lexical: Postgres FTS, OR-recall           │
 │    • vector:  embed query (asymmetric) → cosine │
 │    both UNION page_chunks + file_chunks, each   │
 │    ACL-joined to its LIVE source row; public    │
 │    spaces included but soft-demoted             │
 └───────────────────────────────────────────────┘
    │  two ranked lists
    ▼
 ┌───────────────────────────────────────────────┐
 │ 2. FUSE  Reciprocal Rank Fusion (k=60)         │
 └───────────────────────────────────────────────┘
    │  ~60+ fused candidates
    ▼
 ┌───────────────────────────────────────────────┐
 │ 3. RERANK  (optional, if TELA_RAG_RERANK_URL)  │  rerank.go
 │    cross-encoder re-scores top 50, best-effort  │
 │    (failure → keep fused order)                 │
 └───────────────────────────────────────────────┘
    │  top chunks
    ▼
 ┌───────────────────────────────────────────────┐
 │ 4. ASSEMBLE grounding                          │  askContext (assist.go)
 │    • dedup chunks → sources (pages + files)     │
 │    • front topical HUBS (title- or density-     │
 │      detected) so they expand first             │
 │    • expand chosen pages to FULL body           │
 │      (per-page + budget caps), tail → snippet   │
 │    • label each [n] "Space › path › Title"      │
 │    • attach known-disagreement notes            │
 └───────────────────────────────────────────────┘
    │  numbered, cited excerpt block
    ▼
 ┌───────────────────────────────────────────────┐
 │ 5. GENERATE  LLM answers from grounding only    │  rag.go / ask_job.go
 │    system prompt: cite [n], don't invent, keep  │
 │    projects distinct, surface conflicts;        │
 │    always-on exhaustiveness directive           │
 │    streams token-by-token via a DETACHED job    │
 │    (survives a dropped connection, resumable)   │
 └───────────────────────────────────────────────┘
    │
    ▼
 answer  +  cited sources  +  low-confidence flag  +  follow-up questions
```

Two invariants shape everything (see `rag.go`):

1. **`page_chunks` is a disposable, derived cache of `pages`.** Fully rebuildable;
   carries no authorization state of its own.
2. **Authorize through the live page row, in-query.** Every retrieval joins
   `page_chunks → pages → space_access`. A chunk can never out-live or out-scope
   its page. This is the anti-leak invariant. The readable set is `space_access`
   **plus every `visibility='public'` space** (`accessibleSpacesSQL`, shared by
   `Search`/`ReadChunk`/`ChunkContents`/`PageBodies`/`HubPages`): a public space is
   already world-readable (`/api/public`, SEO), so its chunks belong in the
   ask/search corpus for non-members — that's how a signed-in stranger can ask the
   public **tela Docs** how to use the product. Read-only: publishing still grants
   no `space_access` row and no write, and private spaces stay invisible.
   `accessibleSpacesSQL` carries an `is_member` flag (1 = via `space_access`,
   0 = public-only; a space that's both → 1), and the ranked surfaces **soft-demote**
   public-only content — lexical ts_rank ×`publicRankPenalty`, vector distance
   ×`publicDistPenalty` — so a member page wins a near-tie while a strongly-matching
   public page still ranks. The Luis case (no member match) is unaffected: a uniform
   penalty doesn't change order when every candidate is public.

## Two consumers, one index

The same index serves two audiences that want opposite things — keep both in mind
when changing it:

- **Agents, via MCP** (`research`, `read_chunk`, `get_page`). tela's
  identity is "agents are first-class". The ideal shape here is
  **retrieval-as-a-tool**: a few purposeful tools the agent calls *iteratively* —
  `research` a question (it assembles grounding off the same `askContext` seam the
  web "Ask your docs" path uses), read the best chunk, decide what to ask next.
  Favor crisp citations
  and right-sized chunk reads over one-shot answer assembly.
- **Humans, via the search box + "ask your docs"** (`/api/rag/search`,
  `/api/rag/ask`). Classic one-shot RAG: retrieve top-k → ground an answer →
  cite sources. The web UI uses the **streaming twin** `POST /api/rag/ask/stream`
  (SSE: `meta` → `sources` → `token*` → `followups` → `done`, or an `error` frame)
  so the answer types in live — `/api/rag/ask` stays as the blocking JSON path for
  MCP + non-web clients. Both share retrieval, prompt, and guards; the LLM client
  streams via `llm.Service.CompleteStream` (falling back to a blocking `Complete`
  for providers that don't stream). Answer length is capped by
  `TELA_LLM_MAX_TOKENS` (default 1024) so a slow model can't run past the request
  timeout.
- **Ask generation is a detached job, so a streamed answer survives a dropped
  connection** (`ask_job.go`). An answer takes 10–60s to generate (mostly silent
  LLM prompt-processing before the first token); backgrounded mobile Safari
  suspends the tab's JS and tears down the SSE within ~a second, which used to
  throw the half-built answer away and surface as "the answer model didn't
  respond". So `RAGAskStream` runs retrieval + guards synchronously (clean HTTP
  status on a 429/cap), then spawns generation in a goroutine on a **detached,
  time-bounded context** that fills a replayable event log; the handler just tails
  it. The first `meta` event carries a resume id — if the connection drops
  mid-answer the client reconnects via `GET /api/rag/ask/stream?id=` (no recharge),
  which replays the log from the start and live-tails the rest. The frontend
  (`ask.ts`) drives this: on a torn-down stream it resets the answer and re-attaches
  (deferring to a `visibilitychange` listener while the tab is hidden), so the
  answer continues when the user returns. Jobs live in an in-memory TTL store
  (`askJobTTL`, 10m); generation is bounded by `askGenMaxDuration` (4m).

Neither needs heavyweight machinery (GraphRAG, multi-step agentic pipelines, a
dedicated vector DB). At this corpus scale they'd add fragility, not robustness.

## Robustness model (self-healing index)

The index is best-effort but **self-healing** — an embedder outage degrades to
"stale until it recovers", never a permanent silent gap. See `autoreindex.go`.

- **Debounced auto-reindex.** Page writes call `QueueReindex`; the worker
  reindexes once edits settle (a save burst collapses to one reindex).
- **Retry with backoff.** A failed reindex is re-enqueued with exponential
  backoff (30s → 10m), not dropped. A fresh edit clears the backoff.
- **Stale sweep.** An independent loop periodically re-queues any page whose
  index is missing or out of date (`stalePageIDs` / the shared `staleExpr`). This
  recovers a backlog after an embedder outage *and* after a process restart
  (which loses the in-memory queue but not the corpus).
- **Health logging.** Each sweep logs an `rag: index health` line —
  content/indexed/stale pages, chunk count, model drift — scrapeable by the ops
  stack (Loki/Grafana) without anyone polling the freshness API. `IndexHealth()`
  is the corpus-wide snapshot; per-space/per-page detail is the freshness API
  (`/api/rag/freshness`).

> Lesson from a real incident: the embed endpoint was down for a while, every
> queued reindex failed and was *dropped*, and the stale backlog was invisible
> until someone checked freshness manually. The self-heal + health-log layer
> turns that into "auto-recovers within a sweep, and shows up in the logs".

## Retrieval quality choices

- **Hybrid + RRF.** Lexical and vector fail in opposite directions; fusing them
  (RRF, k=60) is the production baseline. Cheap here because both halves live in
  one Postgres — no second system to operate.
- **OR-recall lexical.** The lexical half rewrites `plainto_tsquery`'s AND to OR,
  so a doc is a candidate if it contains *any* query term, not all — a
  conversational question carries filler words a relevant doc lacks, and
  AND-matching silently zeroed it out of the pool. `ts_rank_cd` still ranks by
  term count + proximity, and rerank supplies precision downstream (`search.go`).
- **Cross-encoder rerank (shipped, off by default).** A second precision stage:
  the top ~50 fused candidates are re-scored by a cross-encoder that reads the
  query and passage *together* (which neither lexical overlap nor embedding
  distance does), lifting the genuinely-relevant above the merely-similar.
  Best-effort with a 5s bound — a slow/failed reranker falls back to the fused
  order and never drags out `/ask`. Enabled by `TELA_RAG_RERANK_URL`
  (Cohere/Jina/TEI-compatible `/rerank`); off ⇒ RRF order is returned as-is. The
  rerank score is also the signal behind the **low-confidence** flag
  (`askLowConfidenceScore`) — a meaning that only exists on the cross-encoder
  scale, so low-confidence is a no-op when rerank is off. See `rerank.go`.
- **Attachments are first-class in retrieval.** Page chunks and file chunks share
  one ranked pool (UNION in both rankers, `file_chunks` id-space ≥ 2^40 so a bare
  chunk id routes to the right table). A file hit cites the attachment (name,
  parent page, download link), gated by the same access join. Text is extracted
  and embedded on every upload/sync path (`index_file.go`).
- **Parent-document expansion.** Chunk retrieval finds the right *neighbourhood*,
  but an answer spanning a whole page (most painfully a registry **table** the
  chunker had to split) can't be rebuilt from one fragment. So the ask path pulls
  a deeper pool (`askRetrieveDepth`), dedups to source pages, and feeds the LLM the
  **full body** of the pages that matter — top-by-rank *plus* content-dense hubs —
  under per-page (`askPageBodyCap`) and cumulative (`askExpandBudget`) caps; the
  long tail degrades to chunk text (`askContext` in `assist.go`).
- **Contextual chunks.** Each chunk's `EmbedText` folds in the page title +
  heading breadcrumb, so an embedded chunk is self-contained (a light form of
  contextual retrieval).
- **Asymmetric query embeddings.** `qwen3-embedding` is instruction-aware:
  queries embed as `Instruct: {task}\nQuery:{q}`, passages bare. `EmbedQuery`
  adds the prefix on the query side only — the corpus is already in the correct
  bare-passage form, so this needs no re-embed. Tune via `TELA_RAG_QUERY_INSTRUCT`
  (set to a single space to disable, e.g. for mxbai).

## Measuring it — `tela rag-eval`

You cannot improve retrieval you don't measure. Score against a golden set of
real (query → expected page) pairs:

```
tela rag-eval --set golden.json [--k 10] [--mode hybrid|semantic|lexical] [--user <id>]
```

Reports **recall@k**, **MRR**, **nDCG@k**, and a per-case hit/rank table. Scoring
runs through the same access-scoped `Search` users hit, so `--user` must be able
to read the spaces under test (defaults to the lowest user id — the bootstrap
admin). Golden set format (`internal/rag/eval.go` → `EvalCase`):

```json
[
  { "query": "how do we deploy a release", "expect_pages": [42] },
  { "query": "reset my password",          "expect_substr": ["password reset"], "space_id": 16 }
]
```

Run it before and after any chunking/embedding/fusion change. Keep the golden set
in version control and grow it whenever a bad retrieval is reported.

## Measuring the *answer* — `tela ask-eval`

`rag-eval` scores **retrieval**. It is structurally blind to a different failure:
the right pages are retrieved, yet the generated **answer drops an item the model
was shown**. The live example: *"which topics are used in UDN, give a table"* —
the page that enumerates every topic is retrieved and expanded to full body, but
the small local generator (a 30B-A3B 4-bit coder model) omits `ufdr-nat`, which
the source mentions only in prose (*"…outputs to the `ufdr-nat` topic"*). Naming
`ufdr-nat` in the question "fixes" it — proof the gap is **generation recall**,
not retrieval. `rag-eval` would score that case 100% and hide the bug.

`ask-eval` runs the **real ask pipeline** (`askContext` → `askSystemPrompt` /
`askUserPrompt` → `llm.Complete`) and checks the answer contains every expected
item, splitting each miss into the two kinds that need different fixes:

```
tela ask-eval --set golden.json [--user <id>] [--answers]
```

- **generation drop** — the item *was* in the assembled grounding but is absent
  from the answer. The model's fault; targeted by `askEnumerationDirective` in
  `askUserPrompt` — an always-on, **self-scoping** completeness instruction ("If
  this asks for a list/table, be exhaustive…"). It's always appended so the model
  decides when it applies, which makes it language-agnostic (it fires on Turkish
  "tabloda ver" too) and a no-op for ordinary Q&A — unlike an English-keyword gate,
  which silently missed non-English enumeration phrasings.
- **retrieval gap** — the item never reached the grounding. Two sub-causes seen:
  the chunk ranked out of the pool, OR (subtler) the page *was* retrieved but its
  full body was never **expanded** — the per-page `askExpandBudget` was spent on
  earlier pages, so the enumerator page degraded to a snippet that omits the
  tail-end items. The fix is `frontHubs` (`askContext`): a **content-dense** page
  (one the query retrieved many chunks from) is treated as a topical hub and
  fronted to expand first — not only **title**-matched pages, since an
  "Architecture Overview" page enumerates the topics without naming them in its
  title. A *real* ranking gap (chunk never retrieved) is still a chunking/fusion
  concern, not generation.

Needs a live embedder **and** LLM (so it exercises the deployed model). Golden set
format (`internal/api/ask_eval.go` → `AskCompletenessCase`; a synthetic sample is
`backend/eval/ask-completeness.example.json` — the real per-corpus set is kept out
of this public repo):

```json
[
  { "question": "which topics are used in the pipeline? give a table",
    "expect_all": ["ingest-raw", "enriched-out", "deadletter"], "space_id": 51 }
]
```

The mean-coverage number plus the drops-vs-gaps split is the before/after metric
for any prompt or generation change. Validate an enumeration fix on *several*
questions (not just the one that was reported) so the change generalises.

## Operations

- **Force a full re-embed** (model name unchanged but the embedder setup moved):
  `tela reindex-all --force`. Bypasses the per-chunk vector cache and re-embeds
  everything — the clean replacement for a manual `TRUNCATE page_chunks`. Without
  `--force`, `reindex-all` reuses cached vectors and only embeds stale/new pages.
- **Resilience.** `reindex-all` skips and counts un-embeddable pages instead of
  aborting; check the `failed` count in the final log line.
- **Model drift.** `embed_model` (migration 0028) stamps each chunk with the
  model that produced it. The health log's `model_drift_chunks` is the count on a
  non-current model — your signal that a re-embed is pending after a model change.
- **Changing the model = re-embed everything** (the chunk hash folds in the model
  name): edit `TELA_RAG_EMBED_MODEL`, redeploy, then `tela reindex-all`.

## Config

| Env | Default | Notes |
| --- | --- | --- |
| `TELA_RAG_EMBED_URL` | — (feature off) | Ollama base; unset ⇒ `/api/rag/*` 503 |
| `TELA_RAG_EMBED_MODEL` | `qwen3-embedding:0.6b` | must be 1024-d |
| `TELA_RAG_EMBED_DIM` | `1024` | advisory; column is fixed `vector(1024)` |
| `TELA_RAG_EMBED_TOKEN` | — | bearer, for the managed cloud endpoint |
| `TELA_RAG_QUERY_INSTRUCT` | sensible default | query-side instruction; single space disables |
| `TELA_RAG_RERANK_URL` | — (rerank off) | full `/rerank` endpoint (Cohere/Jina/TEI shape); set ⇒ second-stage rerank on |
| `TELA_RAG_RERANK_MODEL` | — | optional model name sent to the reranker |
| `TELA_RAG_RERANK_TOKEN` | — | optional bearer for the rerank endpoint |
| `TELA_LLM_MAX_TOKENS` | `1024` | answer length cap (a slow model can't run past the request timeout) |

**Embedder backend is auto-detected** from `TELA_RAG_EMBED_URL`: a `/v1` base
speaks the OpenAI `/embeddings` shape (e.g. a LiteLLM proxy fronting a
primary+relief pool → `embed_openai.go`), anything else is native Ollama
(`embed.go`). The LLM (answer generation) is configured separately via the
`internal/llm` service (`TELA_LLM_*`); Ask needs **both** an embedder and an LLM,
search needs only the embedder.

### Operational guards on the AI paths

- **Fair-use rate limit.** Every embed-touching endpoint (search, ask, draft,
  suggest-links) is per-account rate-limited on the shared embedder — the scarcest
  resource on a single box — returning `429 + Retry-After` (`embedRateOK`).
  Generation is separately bounded per-account (rate + monthly cap,
  `askComputeOK`).
- **Metering.** Every embed call is metered with a length-based token estimate via
  a decorator on the embedder (`recordingEmbedder`), so search/reindex/cloud-proxy
  usage all count; chat + image generation are metered too.
- **AI kill-switch / pause.** An admin switch halts background backfilling and
  puts Ask into an explicit "AI temporarily unavailable" state; a background prober
  also auto-detects an unreachable embedder/LLM (`ai_available`) so a momentary
  outage degrades gracefully instead of throwing a cryptic error. Instant
  full-text search is unaffected throughout.
- **Relief-endpoint failover.** The OpenAI-shaped embed path can front a
  primary+relief pool (LiteLLM), with a per-service health breakdown in the admin
  AI-endpoints view.

## Forward design (the deferred quality track)

Shipped: the robustness layer, the free quality wins, the eval harness, **and
cross-encoder reranking** (see *Retrieval quality choices* — now built,
off-by-default behind `TELA_RAG_RERANK_URL`). What's intentionally *not* built
yet, and the trigger to build it — **measure first** (the eval set is the
precondition for justifying any of these):

- **Fuller contextual retrieval.** An LLM generates a 1–2 sentence "where this
  chunk sits" blurb prepended before embedding (Anthropic reports −49% retrieval
  failures). tela already has the in-process LLM; cost is an index-time LLM call
  per chunk and a re-embed. Gate on the eval set proving it helps *this* corpus.
- **Zero-downtime model migration.** The `embed_model` column is the foundation:
  backfill a new model into parallel tagged rows, eval old-vs-new, then flip —
  instead of `TRUNCATE`-and-go-dark. Build when a model change actually lands.
- **ANN index (HNSW).** *Not yet.* At this corpus size an exact scan is sub-ms and
  gives 100% recall; an ANN index only trades recall for speed you don't need.
  Trigger: vector scan latency becoming material (≫10k–100k chunks). When added,
  pair with pgvector 0.8 iterative scans so the `space_access` filter doesn't
  under-return.
- **Late chunking.** Attractive (no LLM) but needs token-level embeddings +
  custom pooling, which Ollama's `/api/embed` (one pooled vector) doesn't expose.
  Parked unless the embed layer changes.
