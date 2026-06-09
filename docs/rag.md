# RAG — semantic retrieval

How tela turns `pages.body` into searchable meaning, how that index stays
healthy on its own, and how to measure and evolve it. The keystroke-path search
UX (Orama + Postgres FTS) lives in [`search.md`](search.md); this doc is the
embedding/RAG half (`backend/internal/rag`).

## What it is, in one breath

`pages.body` (canonical markdown) → **heading-aware chunks** → **embeddings**
(remote Ollama, `qwen3-embedding:0.6b`, 1024-d) stored in Postgres `pgvector` →
**hybrid retrieval** (lexical Postgres FTS + vector cosine, fused with Reciprocal
Rank Fusion) → ranked chunks, **authorized in-query** through the live page row.

Two invariants shape everything (see `rag.go`):

1. **`page_chunks` is a disposable, derived cache of `pages`.** Fully rebuildable;
   carries no authorization state of its own.
2. **Authorize through the live page row, in-query.** Every retrieval joins
   `page_chunks → pages → space_access`. A chunk can never out-live or out-scope
   its page. This is the anti-leak invariant.

## Two consumers, one index

The same index serves two audiences that want opposite things — keep both in mind
when changing it:

- **Agents, via MCP** (`semantic_search`, `read_chunk`, `get_page`). tela's
  identity is "agents are first-class". The ideal shape here is
  **retrieval-as-a-tool**: a few purposeful tools the agent calls *iteratively* —
  search, read the best chunk, decide what to search next. Favor crisp citations
  and right-sized chunk reads over one-shot answer assembly.
- **Humans, via the search box + "ask your docs"** (`/api/rag/search`,
  `/api/rag/ask`). Classic one-shot RAG: retrieve top-k → ground an answer →
  cite sources.

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
- **Contextual chunks.** Each chunk's `EmbedText` folds in the page title +
  heading breadcrumb, so an embedded chunk is self-contained (a light form of
  contextual retrieval).
- **Asymmetric query embeddings.** `qwen3-embedding` is instruction-aware:
  queries embed as `Instruct: {task}\nQuery:{q}`, passages bare. `EmbedQuery`
  adds the prefix on the query side only — the corpus is already in the correct
  bare-passage form, so this needs no re-embed. Tune via `TELA_RAG_QUERY_INSTRUCT`
  (set to a single space to disable, e.g. for mxbai).
- **Ask grounds on full chunks.** `/api/rag/ask` feeds the LLM each hit's *full*
  chunk text (one access-scoped `ChunkContents` fetch), not the truncated search
  snippet.

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

## Forward design (the deferred quality track)

Shipped: the robustness layer + the two free quality wins + the eval harness.
What's intentionally *not* built yet, and the trigger to build it — **measure
first** (the eval set is the precondition for justifying any of these):

- **Reranking.** Retrieve ~30 hybrid → cross-encoder rerank → top 8. The standard
  precision lever above RRF. Build when the eval set shows top-k *precision* is
  the bottleneck. Cost: one more model to host (e.g. Qwen3-Reranker on the same
  Ollama box).
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
