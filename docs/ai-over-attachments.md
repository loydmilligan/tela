# AI over attachments — design note

**Status:** Slice 1 complete. Indexing foundation (extraction + `file_chunks` +
`ReindexFile`, slice 1a), the auto-index trigger on every upload seam (slice 1b),
and **retrieval** (slice 1c) all landed: file chunks `UNION` into hybrid search,
`read_chunk`, and the Ask path, ACL-gated through the live `space_files` row and
citing the file (name + parent page + `download_url`). Files are now searchable +
answerable. Next: slice 2 (file auto-summaries), slice 3 (office formats via
gotenberg; `related_pages`/`find_overlaps` across files).

## Decisions locked at build time (supersede the recommendations below)

Reading the RAG core changed two calls:

- **Chunk store: B1 (a separate `file_chunks` sibling table), not B2.** `rag.go`
  states `page_chunks` is a deliberately page-pure, disposable cache; an additive
  sibling leaves the deployed page index untouched and `UNION`s in at query time.
  `file_chunks.id` starts at 2^40 so ids never collide with page-chunk ids and
  `read_chunk` routes by range.
- **NO `space_id` on the chunk.** The codebase's **anti-leak invariant** is to
  authorize through the *live* source row; file chunks join `space_files` →
  `space_id` → `space_access`, exactly as page chunks join `pages`. (This reverses
  the "denormalize `space_id`" idea written below — that would violate the
  invariant.)
- **Extraction is pure-Go** (the runtime is distroless — no `pdftotext`):
  `internal/extract`, PDF text-layer via `ledongthuc/pdf` + plaintext; office via
  gotenberg is a later tier.

## The idea in one line

tela already runs a **content → chunks + embeddings + summary** pipeline, but it
is hardwired to `pages`. Generalize it to a **document pipeline** whose source is
pluggable — a **page body** or a **file's extracted text** (and later a
connector's mirrored doc) — and every AI feature lights up for attachments with
no per-feature work: `semantic_search`, `read_chunk`, Ask, `related_pages`,
`find_overlaps`, auto-summaries, freshness/provenance.

The only genuinely new primitive is **bytes → text**. Everything downstream is
reuse.

## Today (what we reuse)

- **RAG:** `page_chunks(page_id, ord, heading_path, content, content_hash,
  embedding vector(1024))` (migration `0002`). `rag` auto-reindexes on page
  change (`internal/rag/autoreindex.go`, `index.go`: `ReindexPage`); `content_hash`
  skips re-embed when unchanged; the chunk/embedding cache key folds in the
  **embed-model name** (so a model change = full re-embed; `reindex-all`).
- **Summaries:** `internal/summarize` is the *"generation sibling to rag's
  auto-reindex"* — one field on `api.Server` (`s.summarize`), `Queue(pageID)`
  fired from `createPageCore`/`updatePageCore` (`pages.go:469,695`); a 1–2 sentence
  `summary` page property kept fresh as the body changes.
- **Retrieval ACL:** `internal/rag/search.go` scopes every query by
  `JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = …)` — a chunk
  is visible iff the caller can access its space.
- **File store:** `space_files` (migration `0015`), content-addressed by
  `content_hash`, parented to a page. `createPageUploadFile` dedups by hash.

## Design

### 1. Document-source abstraction (fork B — files as first-class documents)

A chunk gains a **source**: it belongs to a page *or* a file. Two options:

- **B1 — parallel `file_chunks` table** mirroring `page_chunks`, `UNION`ed in
  search. Additive, zero risk to the working page index, but duplicates the chunk
  machinery + every query.
- **B2 — generalize the chunk store** to carry `(source_kind, source_id)` and a
  denormalized `space_id`. One table, one query path, one indexer. Bigger
  migration (touches the live page RAG), but it's the version that makes
  *connectors* free later too.

**Recommendation: B2.** Concretely:
- Chunk row carries `source_kind ('page'|'file')`, `source_id`, and **`space_id`
  denormalized** so the `space_access` JOIN is source-agnostic (no per-source ACL
  logic). `ON DELETE CASCADE` from both `pages` and `space_files`.
- `semantic_search`/`read_chunk` results carry the source so a citation points at
  the **file** (name + `download_url` + parent page), not a phantom page.
- Content-addressed bonus: a file's chunks are keyed by its `content_hash`, so a
  file attached to N pages is **indexed once** — dedup extends to the index.

### 2. Text extraction (the one new primitive)

`extractText(mime, bytes) -> (text, ok)`:
- **PDF** → poppler `pdftotext` (or a Go lib); OCR (tesseract) opt-in for scans.
- **docx/pptx/xlsx** → the **gotenberg** LibreOffice route we already deploy
  (→ PDF → text), or pandoc. *(Phase 2.)*
- **txt/md/csv/json** → trivial passthrough.
- Failure (encrypted, scanned-no-OCR, binary) → **no index, file still attaches +
  previews**. Never block the upload on extraction.
- Cap extracted text (e.g. reuse `summarizeMaxBodyChars`-style caps); skip
  oversize/binary files by policy.

Runs **async**, exactly like summary generation does today.

### 3. Triggers — unify the two file-write seams

There are two places a file is written today:
- `createPageUploadFile` — the funnel for editor drag-drop, MCP `upload_attachment`
  (base64), **and** the signed-PUT handshake (all three already go through it ✓).
- **WebDAV sync** has its **own** `INSERT INTO space_files` (`dav_space_files.go:155`).

**Refactor both into one `storeSpaceFile(...)` "store-and-announce" seam** that, on
a content change (new `content_hash`), enqueues extraction → reindex → summarize —
mirroring the page path's `s.summarize.Queue` + rag auto-reindex. Then a **single
hook covers every ingress**: editor, MCP base64, handshake PUT, and rclone sync.
Soft-delete tears the file's chunks/summary down (like a page delete cascades
`page_chunks`).

Change-detection is **exact and free**: identical bytes dedup at
`createPageUploadFile`, so the trigger only fires on real content change — no
diffing, no wasted re-embed on a re-sync.

### 4. What lights up for free

| Feature | For files, with zero new per-feature code |
| --- | --- |
| `semantic_search` / `read_chunk` | finds inside PDFs/docs, ACL-gated, cites the file |
| Ask | answers from attachment content |
| `related_pages` / `suggest_links` | a file ↔ pages by similarity |
| `find_overlaps` | a file duplicating a page's content |
| auto-summary | "this 40-page PDF is a vendor MSA about X" on the card/hover |
| freshness / provenance | `updated_at`, uploaded-by / sync — feeds the trust layer; an agent MCP upload carries agent-provenance → shows in the "changes by your AI" feed |

### 5. User-facing surfaces

- Search results include file hits (badge + parent page + open).
- File card / hover shows the generated summary.
- Ask citations link to the file's `download_url` (+ in-page PDF preview).

## Watch-outs

- **Re-embed cost:** file chunks join the same model-hash story; a model change
  re-embeds files too (`reindex-all` already resumable).
- **ACL leak:** retrieval MUST gate file chunks by the file's space exactly like
  pages — the denormalized `space_id` + the existing `space_access` JOIN handles
  this uniformly; do not add an app-perm bypass.
- **Big/binary files:** cap extracted text, skip non-text by policy, OCR opt-in.
- **Embedder dependency:** RAG already no-ops (503) without `TELA_RAG_EMBED_URL`;
  file indexing inherits that — extraction can still run for summaries, or both
  no-op gracefully.

## Phasing

1. **Slice 1 (core):** generalize the chunk store (B2) + PDF/plaintext extraction
   + the unified store-and-announce seam → files appear in `semantic_search` /
   `read_chunk` / Ask, ACL-gated, citing the file.
2. **Slice 2:** file auto-summaries (reuse `summarize`) on the card/hover.
3. **Slice 3:** office formats via gotenberg; `related_pages`/`find_overlaps`
   across files.
4. **Later:** connector-mirrored docs become a third `source_kind` — free.

## Open decisions (for sign-off)

1. **Chunk store: B2 (generalize `page_chunks`) vs B1 (parallel `file_chunks` +
   UNION)?** — appetite for touching the live page index vs. an additive table.
   *(Recommend B2.)*
2. **Extraction scope for Slice 1: PDF + plaintext only, office (gotenberg) as
   Phase 3?** *(Recommend yes — PDF+txt first.)*
3. **Seam: refactor WebDAV sync to share `storeSpaceFile` now, or hook both seams
   separately?** *(Recommend unify now — it's the whole point.)*
4. **File summaries: a `summary` column on `space_files` (Slice 2), shown on the
   card?** *(Recommend yes, as Slice 2.)*
