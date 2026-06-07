package rag

import (
	"context"
	"database/sql"
	"errors"
)

// ErrChunkNotFound is returned when a chunk id doesn't exist OR the caller can't
// access its page. The two are deliberately indistinguishable so a probing
// caller can't learn that a chunk exists in a space they can't read.
var ErrChunkNotFound = errors.New("chunk not found")

// ChunkRead is the full text of one chunk plus its citation. Unlike a search
// Hit (which carries a truncated snippet), this returns the complete section
// content — the chunk-granularity read between snippet and whole-page get_page.
type ChunkRead struct {
	ChunkID     int64  `json:"chunk_id"`
	PageID      int64  `json:"page_id"`
	SpaceID     int64  `json:"space_id"`
	Title       string `json:"title"`
	HeadingPath string `json:"heading_path"`
	Content     string `json:"content"`
	UpdatedAt   string `json:"updated_at"`
}

// ReadChunk returns one chunk's full content, authorized through the LIVE page
// row joined to space_access (the anti-leak invariant — same as search). spaceID,
// when non-nil, additionally narrows to that space (space-scoped bearer keys).
// Returns ErrChunkNotFound when the chunk doesn't exist or is out of scope.
func (s *Service) ReadChunk(ctx context.Context, userID, chunkID int64, spaceID *int64) (*ChunkRead, error) {
	qb := &queryBuilder{}
	cid := qb.arg(chunkID)
	uid := qb.arg(userID)
	query := `
		SELECT pc.id, pc.page_id, p.space_id, pc.heading_path, pc.content, p.title, p.updated_at
		  FROM page_chunks pc
		  JOIN pages p ON p.id = pc.page_id AND p.deleted_at IS NULL
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = ` + uid + `) sm
		    ON sm.space_id = p.space_id
		 WHERE pc.id = ` + cid
	if spaceID != nil {
		query += ` AND p.space_id = ` + qb.arg(*spaceID)
	}

	var c ChunkRead
	err := s.db.QueryRowContext(ctx, query, qb.args...).Scan(
		&c.ChunkID, &c.PageID, &c.SpaceID, &c.HeadingPath, &c.Content, &c.Title, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChunkNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
