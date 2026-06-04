# internal/rag — semantic retrieval (experimental)

Self-contained RAG layer for tela: heading-aware markdown chunking, pluggable
embeddings (Ollama by default), and **hybrid** chunk search (BM25 + cosine,
RRF-fused) scoped to the caller's `space_access`. Kept deliberately isolated so
we can iterate without touching load-bearing code.

## What it touches outside this package (minimal, by design)

- `migrations/0019_rag_chunks.sql` — `page_chunks` + `page_chunks_fts` (additive; no triggers on `pages`).
- `api/api.go` — one `rag *rag.Service` field, built in `New()`. Importing this package also registers the `tela_cosine` UDF (`vector.go` `init`).
- `api/rag.go` + `router.go` — two routes: `GET /api/rag/search`, `POST /api/rag/reindex`.
- `mcp/src/tools/semantic-search.ts` — the agent-facing `semantic_search` tool.

Nothing in the page create/update path imports `rag`. Indexing is **on-demand**
(reindex), not write-path-coupled — that's a deliberate phase-1 choice so we
don't perturb the hot path while experimenting.

## Ships dark

With `TELA_RAG_EMBED_URL` unset: `Service.Enabled()` is false, the endpoints
503, and the MCP tool errors. Set it to enable. See `deploy/.env.example`.

## How it works

```
page body ──ChunkMarkdown──▶ chunks (heading-aware, contextual prefix)
                                  │  embed each via Ollama (mxbai-embed-large, 1024-d)
                                  ▼
                          page_chunks(embedding BLOB)  +  page_chunks_fts
                                  │
query ──▶ lexicalRank (BM25)  ┐
      └─▶ vectorRank (tela_cosine kNN) ┘──RRF(k=60)──▶ ranked Hits (page_id + heading_path)
```

`tela_cosine(a_blob, b_blob)` is a Go scalar UDF (same mechanism as
`tela_strip_excalidraw`) — brute-force cosine inside SQLite, so vector search is
just an `ORDER BY` with no extra moving parts. We use a UDF rather than the
`sqlite-vec` C extension only because the current driver (`modernc.org/sqlite`)
is pure Go and can't load C extensions — a practical fact about today's build,
not a constraint to defend. At wiki scale (thousands–tens-of-thousands of
chunks) brute force is sub-10ms. If the corpus ever outgrows it, revisit with a
real ANN index (e.g. a cgo driver + `sqlite-vec`, or an external index).

## Try it

```bash
# 1. enable + point at an Ollama
export TELA_RAG_EMBED_URL=http://tardis:11434   # mxbai-embed-large already pulled

# 2. live end-to-end smoke test (seeds pages, reindexes, searches)
cd backend
TELA_RAG_SMOKE=1 go test ./internal/rag/ -run TestSmoke -v

# 3. against a running server
curl -XPOST 'localhost:8080/api/rag/reindex?space_id=3'      # backfill a space
curl 'localhost:8080/api/rag/search?q=how+do+we+deploy&space_id=3'
```

## Next (not built yet)

- Reranking stage (cross-encoder) on the fused top-N.
- Write-path hook (reindex a page on save) once the shape is settled.
- Optional in-app "Ask tela" answer layer over this same core.
- Eval harness (golden Q&A, retrieval hit-rate).
