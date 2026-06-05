# internal/rag — semantic retrieval

Self-contained hybrid retrieval over `pages`: heading-aware markdown chunking,
pluggable embeddings, and RRF-fused (lexical + vector) chunk search on
Postgres + pgvector. Agent-first — no answer-LLM; every hit carries a citation
(`page_id` + `heading_path`).

## Ships dark

Unconfigured by default. With `TELA_RAG_EMBED_URL` unset the service is
`Enabled() == false`, no embeddings are computed, and `/api/rag/*` return `503`.
Set the env to enable:

```
TELA_RAG_EMBED_URL=http://tardis:11434   # Ollama base
TELA_RAG_EMBED_MODEL=mxbai-embed-large   # default; 1024-d
```

## Endpoints

| Route | Scope | Purpose |
|---|---|---|
| `GET /api/rag/search?q=&space_id=&limit=&mode=` | session / bearer-read | hybrid chunk search; `mode` = `hybrid` (default) \| `lexical` \| `semantic` |
| `POST /api/rag/reindex?space_id=` | membership / bearer-write | chunk + embed every page in a space |

## Invariants (do not break)

- **Disposable cache.** `page_chunks` is fully rebuildable from `pages` via
  `ReindexPage` / `ReindexSpace`. No source-of-truth state lives here.
- **Authorize through the live page row.** Every query joins
  `page_chunks → pages → space_access`; there is deliberately **no `space_id`
  column on `page_chunks`**. A chunk can never out-scope its page.
- **Embedding is off the hot path.** Embeddings are an external network op; the
  `content_hash` (model + embed-text) lets reindex skip unchanged chunks.

## Pieces

- `chunk.go` — heading-aware splitter (+ Excalidraw fence strip, contextual prefix).
- `embed.go` — `OllamaEmbedder` (`/api/embed`) + pgvector text-literal helper.
- `index.go` — `ReindexPage` / `ReindexSpace` (hash-keyed embedding reuse).
- `search.go` — vector (`<=>`) + lexical (`tsvector`/`ts_rank_cd`) ranks, RRF-fused.

## Tests

`go test ./internal/rag` runs the unit + DB-backed tests with a fake embedder.
The live semantic check needs Ollama:

```
TELA_RAG_EMBED_URL=http://tardis:11434 go test ./internal/rag -run Smoke -v
```

## Schema

Migrations `0002_rag.sql` (extension + `page_chunks` + `embedding vector(1024)`)
and `0003_chunk_fts.sql` (`content_tsv` generated column + GIN). No ANN index
yet — exact scan at wiki scale; add HNSW/IVFFlat when a seq scan stops being
enough (see the RAG journey doc).
