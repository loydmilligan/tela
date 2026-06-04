# Search (design notes — NOT yet built)

Status: **placeholder in production.** The SQLite FTS5 search was removed during
the Postgres switch. `GET /api/search` and `GET /api/search/bodies` currently
run an unranked `ILIKE` substring scan (see the `TODO(search)` banners in
`backend/internal/api/search.go` / `search_bodies.go`). This document is the
target we design toward — do not treat the current behavior as final.

## Goals

1. **Unbelievably instant.** Results update on every keystroke with no
   perceptible latency. The enemy is the network round-trip, not the engine.
2. **Semantic, improving over time.** As the corpus grows and gets embedded,
   search should understand meaning, not just substrings.
3. **No model calls on the keystroke path.** User-side typing must never trigger
   Ollama/embedding work — that breaks the instant feel.

## Architecture (two tiers)

**Tier 1 — instant lexical (client-side, zero network).**
A synced in-browser index (Orama is already in the frontend deps) answers every
keystroke locally. This is what makes it feel instant. The server feeds it a
compact index payload; it never round-trips per keystroke.

**Tier 2 — semantic refinement (server-side, debounced).**
Fires on a pause / Enter, not per keystroke. Re-ranks and augments the Tier-1
results with meaning-based hits. Embeddings are computed **at write time**
(page saved → embed in the background via Ollama), stored in Postgres
`pgvector`. At query time the only online model cost is embedding the query
string once — kept off the keystroke path. Results stream in and re-order the
already-shown instant results ("over-time refinement").

## Postgres is the foundation (all in one engine)

- `tsvector` / `to_tsquery` + GIN — ranked lexical full-text (`ts_rank_cd`,
  `ts_headline` for snippets). Replaces FTS5 `bm25()`/`snippet()`.
- `pg_trgm` + GIN — fuzzy / typo / substring / prefix matching.
- `pgvector` (`CREATE EXTENSION vector`) — semantic. HNSW or IVFFlat index.

A single hybrid SQL query can blend lexical rank + vector distance. No new
infrastructure beyond enabling the extensions — this is the whole reason the
Postgres switch is worth it for search.

## Carry-overs to reimplement when this is built

- **Excalidraw strip.** Page bodies contain ` ```excalidraw\n{json}\n``` `
  fences. Before the SQLite FTS5 era these were stripped from the indexed text
  by `tela_strip_excalidraw` (a Go UDF, since deleted) so drawing JSON didn't
  pollute search. The strip logic (regex in the old `db/sqlite_funcs.go`,
  recoverable from git history) must be reapplied to whatever text feeds the
  tsvector / embedding pipeline.
- **Access control.** Both endpoints join `space_access` (and honor the bearer
  `space_id` restriction). Any rewrite must preserve that — never surface a hit
  from a space the caller can't open.
- **Response contracts.** `/api/search` → `{results:[{page_id,space_id,title,
  snippet,breadcrumb}]}`; `/api/search/bodies` → `{results:[{id,title,score}]}`
  with score higher = better. Keep these stable so the FE + MCP tool don't move.
