package rag

import (
	"context"
	"database/sql"
)

// Related pages — "see also" for a wiki. Uses the embeddings already in
// page_chunks (no live embedder needed): the source page's vector CENTROID vs
// every other page's nearest chunk, access-scoped through the live page row.
// This is the headline knowledge-intelligence feature — semantic neighbors beat
// hand-maintained "related" links and backlinks for discovery.

// RelatedPage is one semantically-related page (never the source page itself).
type RelatedPage struct {
	PageID     int64   `json:"page_id"`
	SpaceID    int64   `json:"space_id"`
	Title      string  `json:"title"`
	Similarity float64 `json:"similarity"` // cosine similarity in [0,1], higher = closer
	UpdatedAt  string  `json:"updated_at"`
}

// RelatedPages returns the pages most semantically similar to pageID, ranked by
// the closest chunk to the source page's embedding centroid. Scoped to spaces
// userID can read (both the source page and the candidates — a page the caller
// can't read yields an empty result, no leak). spaceID, when non-nil, restricts
// candidates to that one space ("related within this space"). Returns an empty
// slice when the page has no embedded chunks (empty/unindexed). Needs stored
// vectors but NOT a live embedder, so it works even while the embedder is down.
func (s *Service) RelatedPages(ctx context.Context, userID, pageID int64, spaceID *int64, limit int) ([]RelatedPage, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	// Centroid of the source page's vectors, computed access-scoped so a caller
	// can't probe a page they can't read. Cosine distance is scale-invariant, so
	// the un-normalized average is a valid query vector.
	var centroid sql.NullString
	if err := s.db.QueryRowContext(ctx, `
		SELECT avg(pc.embedding)::text
		  FROM page_chunks pc
		  JOIN pages p ON p.id = pc.page_id AND p.deleted_at IS NULL
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $2) sm
		    ON sm.space_id = p.space_id
		 WHERE pc.page_id = $1 AND pc.embedding IS NOT NULL`,
		pageID, userID,
	).Scan(&centroid); err != nil {
		return nil, err
	}
	if !centroid.Valid {
		return []RelatedPage{}, nil
	}
	return s.nearestPagesByVector(ctx, userID, centroid.String, &pageID, spaceID, limit)
}

// nearestPagesByVector ranks pages by their closest chunk to a query vector
// (pgvector cosine), access-scoped through space_access. excludePage drops the
// source page (nil keeps all). spaceID narrows to one space. The shared core
// behind RelatedPages (centroid) and SuggestLinks (embedded draft text).
func (s *Service) nearestPagesByVector(ctx context.Context, userID int64, vecLit string, excludePage *int64, spaceID *int64, limit int) ([]RelatedPage, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	qb := &queryBuilder{}
	cen := qb.arg(vecLit)
	uid := qb.arg(userID)
	q := `
		SELECT p.id, p.space_id, p.title, p.updated_at,
		       MIN(pc.embedding <=> ` + cen + `::vector) AS dist
		  FROM page_chunks pc
		  JOIN pages p ON p.id = pc.page_id AND p.deleted_at IS NULL
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = ` + uid + `) sm
		    ON sm.space_id = p.space_id
		 WHERE pc.embedding IS NOT NULL`
	if excludePage != nil {
		q += ` AND pc.page_id <> ` + qb.arg(*excludePage)
	}
	if spaceID != nil {
		q += ` AND p.space_id = ` + qb.arg(*spaceID)
	}
	q += `
		 GROUP BY p.id, p.space_id, p.title, p.updated_at
		 ORDER BY dist ASC
		 LIMIT ` + qb.arg(limit)

	rows, err := s.db.QueryContext(ctx, q, qb.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RelatedPage{}
	for rows.Next() {
		var r RelatedPage
		var dist float64
		if err := rows.Scan(&r.PageID, &r.SpaceID, &r.Title, &r.UpdatedAt, &dist); err != nil {
			return nil, err
		}
		r.Similarity = 1 - dist // cosine distance → similarity
		out = append(out, r)
	}
	return out, rows.Err()
}
