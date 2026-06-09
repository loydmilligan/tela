package rag

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

// ChunkContents returns the FULL section text for a set of chunk ids, keyed by
// chunk id, authorized through the live page row joined to space_access (the
// same anti-leak path as ReadChunk/Search). Out-of-scope or missing ids are
// silently omitted from the map — callers fall back to whatever they have. Used
// by "ask your docs" to ground the LLM on whole chunks instead of the truncated
// search snippet. One query, not N — safe to call with the top-k hit ids.
func (s *Service) ChunkContents(ctx context.Context, userID int64, ids []int64, spaceID *int64) (map[int64]string, error) {
	if len(ids) == 0 {
		return map[int64]string{}, nil
	}
	qb := &queryBuilder{}
	uid := qb.arg(userID)
	ph := make([]string, len(ids))
	for i, id := range ids {
		ph[i] = qb.arg(id)
	}
	query := `
		SELECT pc.id, pc.content
		  FROM page_chunks pc
		  JOIN pages p ON p.id = pc.page_id AND p.deleted_at IS NULL
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = ` + uid + `) sm
		    ON sm.space_id = p.space_id
		 WHERE pc.id IN (` + strings.Join(ph, ",") + `)`
	if spaceID != nil {
		query += ` AND p.space_id = ` + qb.arg(*spaceID)
	}
	rows, err := s.db.QueryContext(ctx, query, qb.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]string, len(ids))
	for rows.Next() {
		var id int64
		var content string
		if err := rows.Scan(&id, &content); err != nil {
			return nil, err
		}
		out[id] = content
	}
	return out, rows.Err()
}
