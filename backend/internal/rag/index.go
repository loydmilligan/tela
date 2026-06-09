package rag

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
)

// chunkHash keys a chunk's embedding by (model, embed-text) so a reindex can
// reuse a stored vector when nothing relevant changed. Folding the model in
// means switching embedders invalidates every cached vector automatically.
func chunkHash(model, embedText string) string {
	h := sha256.Sum256([]byte(model + "\x00" + embedText))
	return hex.EncodeToString(h[:])
}

// ReindexPage rebuilds page_chunks for one page: chunk → (reuse cached vector or
// embed) → replace rows in a single transaction. Idempotent; unchanged chunks
// reuse their stored vector and never re-hit the embedder. Returns the number of
// chunks written.
func (s *Service) ReindexPage(ctx context.Context, pageID int64) (int, error) {
	return s.reindexPage(ctx, pageID, false)
}

// reindexPage is ReindexPage with an explicit force flag. force=true bypasses the
// per-chunk vector cache so every chunk is re-embedded against the CURRENT
// embedder — the clean way to force a full re-embed after an embedder setup
// change that the model-name-keyed cache can't see (replaces a manual TRUNCATE).
func (s *Service) reindexPage(ctx context.Context, pageID int64, force bool) (int, error) {
	if !s.Enabled() {
		return 0, fmt.Errorf("rag: embedder not configured")
	}

	var title, body string
	if err := s.db.QueryRowContext(ctx,
		`SELECT title, body FROM pages WHERE id = $1`, pageID,
	).Scan(&title, &body); err != nil {
		// Page deleted between enqueue and reindex — benign; its chunks were
		// already removed by ON DELETE CASCADE. Nothing to index.
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}

	chunks := ChunkMarkdown(title, StripExcalidrawFences(body))
	cached := map[string]string{}
	if !force {
		var err error
		if cached, err = s.cachedVectors(ctx, pageID); err != nil {
			return 0, err
		}
	}

	model := s.emb.Model()
	type row struct {
		ord                    int
		hp, content, hash, emb string // emb is a pgvector literal "[...]"
	}
	rows := make([]row, 0, len(chunks))
	for _, c := range chunks {
		hash := chunkHash(model, c.EmbedText)
		emb, ok := cached[hash]
		if !ok {
			vec, err := s.emb.Embed(ctx, c.EmbedText)
			if err != nil {
				return 0, fmt.Errorf("embed chunk %d of page %d: %w", c.Ord, pageID, err)
			}
			emb = vecLiteral(vec)
		}
		rows = append(rows, row{c.Ord, c.HeadingPath, c.Content, hash, emb})
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM page_chunks WHERE page_id = $1`, pageID); err != nil {
		return 0, err
	}
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO page_chunks
			  (page_id, ord, heading_path, content, content_hash, embedding, embed_model)
			VALUES ($1, $2, $3, $4, $5, $6::vector, $7)`,
			pageID, r.ord, r.hp, r.content, r.hash, r.emb, model,
		); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// cachedVectors returns the (content_hash -> embedding literal) map already
// stored for a page, used to skip re-embedding unchanged chunks across reindex
// runs. embedding::text renders the pgvector value back as "[...]" so it can be
// re-inserted via a ::vector cast without re-embedding.
func (s *Service) cachedVectors(ctx context.Context, pageID int64) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT content_hash, embedding::text FROM page_chunks WHERE page_id = $1 AND embedding IS NOT NULL`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var h, e string
		if err := rows.Scan(&h, &e); err != nil {
			return nil, err
		}
		m[h] = e
	}
	return m, rows.Err()
}

// ReindexSpace reindexes every page in a space, page by page. Returns the number
// of pages processed and chunks written. Resilient: a single page that fails to
// embed is logged and skipped, not fatal — one bad page never aborts the run.
// err is returned only for an infrastructure failure (listing the pages).
func (s *Service) ReindexSpace(ctx context.Context, spaceID int64) (pages, chunks int, err error) {
	pages, chunks, _, err = s.reindexSpace(ctx, spaceID, false)
	return pages, chunks, err
}

// reindexSpace is ReindexSpace with a force flag and a failed-page count. ctx
// cancellation aborts the run (returns ctx.Err()); per-page embed failures are
// counted and skipped.
func (s *Service) reindexSpace(ctx context.Context, spaceID int64, force bool) (pages, chunks, failed int, err error) {
	if !s.Enabled() {
		return 0, 0, 0, fmt.Errorf("rag: embedder not configured")
	}
	ids, err := s.pageIDs(ctx, spaceID)
	if err != nil {
		return 0, 0, 0, err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return pages, chunks, failed, err
		}
		n, err := s.reindexPage(ctx, id, force)
		if err != nil {
			slog.Error("rag: reindex page failed (skipping)", "space_id", spaceID, "page_id", id, "err", err)
			failed++
			continue
		}
		pages++
		chunks += n
	}
	return pages, chunks, failed, nil
}

// ReindexSummary is the result of a whole-corpus reindex (the reindex-all CLI).
type ReindexSummary struct {
	Spaces, Pages, Chunks, Failed int
}

// ReindexAll re-embeds every page in every space against the current embedder,
// logging per-space progress. force=true bypasses the per-chunk cache (full
// re-embed). Resilient: a failing page is skipped and counted, never aborting
// the run; only an infrastructure failure (listing spaces/pages) returns err.
func (s *Service) ReindexAll(ctx context.Context, force bool) (ReindexSummary, error) {
	var sum ReindexSummary
	if !s.Enabled() {
		return sum, fmt.Errorf("rag: embedder not configured")
	}
	type spaceRef struct {
		id   int64
		name string
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM spaces ORDER BY id`)
	if err != nil {
		return sum, fmt.Errorf("list spaces: %w", err)
	}
	var spaces []spaceRef
	for rows.Next() {
		var sp spaceRef
		if err := rows.Scan(&sp.id, &sp.name); err != nil {
			rows.Close()
			return sum, err
		}
		spaces = append(spaces, sp)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sum, err
	}

	slog.Info("reindex-all: starting", "spaces", len(spaces), "model", s.emb.Model(), "force", force)
	for i, sp := range spaces {
		pages, chunks, failed, err := s.reindexSpace(ctx, sp.id, force)
		if err != nil {
			return sum, fmt.Errorf("space %d (%s): %w", sp.id, sp.name, err)
		}
		sum.Spaces++
		sum.Pages += pages
		sum.Chunks += chunks
		sum.Failed += failed
		slog.Info("reindex-all: space done",
			"progress", i+1, "total", len(spaces), "space_id", sp.id, "name", sp.name,
			"pages", pages, "chunks", chunks, "failed", failed)
	}
	slog.Info("reindex-all: DONE",
		"spaces", sum.Spaces, "pages", sum.Pages, "chunks", sum.Chunks, "failed", sum.Failed, "model", s.emb.Model())
	return sum, nil
}

func (s *Service) pageIDs(ctx context.Context, spaceID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM pages WHERE space_id = $1 AND deleted_at IS NULL ORDER BY id`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
