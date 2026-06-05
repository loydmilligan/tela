package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	if !s.Enabled() {
		return 0, fmt.Errorf("rag: embedder not configured")
	}

	var title, body string
	if err := s.db.QueryRowContext(ctx,
		`SELECT title, body FROM pages WHERE id = $1`, pageID,
	).Scan(&title, &body); err != nil {
		return 0, err
	}

	chunks := ChunkMarkdown(title, StripExcalidrawFences(body))
	cached, err := s.cachedVectors(ctx, pageID)
	if err != nil {
		return 0, err
	}

	type row struct {
		ord                    int
		hp, content, hash, emb string // emb is a pgvector literal "[...]"
	}
	rows := make([]row, 0, len(chunks))
	for _, c := range chunks {
		hash := chunkHash(s.emb.Model(), c.EmbedText)
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
			  (page_id, ord, heading_path, content, content_hash, embedding)
			VALUES ($1, $2, $3, $4, $5, $6::vector)`,
			pageID, r.ord, r.hp, r.content, r.hash, r.emb,
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
// of pages processed and chunks written. Synchronous — fine for a team-wiki
// corpus; the embed calls dominate wall-clock.
func (s *Service) ReindexSpace(ctx context.Context, spaceID int64) (pages, chunks int, err error) {
	if !s.Enabled() {
		return 0, 0, fmt.Errorf("rag: embedder not configured")
	}
	ids, err := s.pageIDs(ctx, spaceID)
	if err != nil {
		return 0, 0, err
	}
	for _, id := range ids {
		n, err := s.ReindexPage(ctx, id)
		if err != nil {
			return pages, chunks, fmt.Errorf("reindex page %d: %w", id, err)
		}
		pages++
		chunks += n
	}
	return pages, chunks, nil
}

func (s *Service) pageIDs(ctx context.Context, spaceID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM pages WHERE space_id = $1 ORDER BY id`, spaceID)
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
