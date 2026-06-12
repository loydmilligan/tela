package rag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/zcag/tela/backend/internal/extract"
)

// index_file.go — the file half of the document index, sibling to index.go's
// ReindexPage. A page's source text is its body; a file's is its EXTRACTED text
// (PDF/plaintext). Everything else — heading-aware chunking, the per-chunk
// vector cache keyed by (model, text), the replace-in-one-tx write — is shared.

// ReindexFile rebuilds file_chunks for one attachment: load + extract → chunk →
// (reuse cached vector or embed) → replace rows in a transaction. Files whose
// bytes aren't text-extractable (images, binaries, scanned PDFs) index to zero
// chunks — benign, and any stale chunks are cleared. Returns chunks written.
func (s *Service) ReindexFile(ctx context.Context, fileID int64) (int, error) {
	if !s.Enabled() {
		return 0, fmt.Errorf("rag: embedder not configured")
	}

	var name, mime string
	var data []byte
	var deletedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT name, mime, data, deleted_at FROM space_files WHERE id = $1`, fileID,
	).Scan(&name, &mime, &data, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil // gone between enqueue and reindex; chunks cascade-deleted
	}
	if err != nil {
		return 0, err
	}

	// Soft-deleted or not text-extractable → ensure no stale chunks, done.
	if deletedAt.Valid {
		return 0, s.clearFileChunks(ctx, fileID)
	}
	text, ok := extract.Text(mime, name, data)
	if !ok || text == "" {
		return 0, s.clearFileChunks(ctx, fileID)
	}

	chunks := ChunkMarkdown(name, text)
	cached, err := s.cachedFileVectors(ctx, fileID)
	if err != nil {
		return 0, err
	}

	model := s.emb.Model()
	type row struct {
		ord                    int
		hp, content, hash, emb string
	}
	rows := make([]row, 0, len(chunks))
	for _, c := range chunks {
		hash := chunkHash(model, c.EmbedText)
		emb, ok := cached[hash]
		if !ok {
			vec, err := s.emb.Embed(ctx, c.EmbedText)
			if err != nil {
				return 0, fmt.Errorf("embed chunk %d of file %d: %w", c.Ord, fileID, err)
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM file_chunks WHERE space_file_id = $1`, fileID); err != nil {
		return 0, err
	}
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO file_chunks
			  (space_file_id, ord, heading_path, content, content_hash, embedding, embed_model)
			VALUES ($1, $2, $3, $4, $5, $6::vector, $7)`,
			fileID, r.ord, r.hp, r.content, r.hash, r.emb, model,
		); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (s *Service) clearFileChunks(ctx context.Context, fileID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM file_chunks WHERE space_file_id = $1`, fileID)
	return err
}

// cachedFileVectors mirrors cachedVectors for files: (content_hash → embedding
// literal) already stored, so an unchanged chunk skips the embedder on reindex.
func (s *Service) cachedFileVectors(ctx context.Context, fileID int64) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT content_hash, embedding::text FROM file_chunks WHERE space_file_id = $1 AND embedding IS NOT NULL`, fileID)
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
