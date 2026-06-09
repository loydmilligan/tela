package rag

import "context"

// Overlap / near-duplicate detection — wiki hygiene. As a knowledge base grows,
// the same topic gets re-documented in two or three places that then drift out of
// sync. Guru and Slite make "verification" a headline; the upstream problem is
// duplication. This finds page PAIRS whose embedding centroids are close enough
// to be candidates for merge/redirect — so the corpus can be kept DRY instead of
// silently fragmenting. Centroid-vs-centroid (one row per page), so it's an
// n²-over-pages scan that stays cheap at wiki scale.

// OverlapPair is two pages whose content is semantically close.
type OverlapPair struct {
	PageA      int64   `json:"page_a"`
	TitleA     string  `json:"title_a"`
	PageB      int64   `json:"page_b"`
	TitleB     string  `json:"title_b"`
	SpaceA     int64   `json:"space_a"`
	SpaceB     int64   `json:"space_b"`
	Similarity float64 `json:"similarity"` // cosine similarity of centroids, [0,1]
}

// FindOverlaps returns page pairs whose centroids are at least `threshold`
// similar, ranked closest-first, access-scoped through space_access. spaceID,
// when non-nil, restricts to overlaps WITHIN one space (the common "is this
// space full of duplicates?" case). A pair where the caller can't read both
// pages never appears. threshold defaults to 0.55; limit to 50.
func (s *Service) FindOverlaps(ctx context.Context, userID int64, spaceID *int64, threshold float64, limit int) ([]OverlapPair, error) {
	if threshold <= 0 || threshold > 1 {
		threshold = 0.55
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	maxDist := 1 - threshold

	qb := &queryBuilder{}
	uid := qb.arg(userID)
	cte := `
		cent AS (
		  SELECT pc.page_id, p.space_id, p.title, avg(pc.embedding) AS c
		    FROM page_chunks pc
		    JOIN pages p ON p.id = pc.page_id AND p.deleted_at IS NULL
		    JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = ` + uid + `) sm
		      ON sm.space_id = p.space_id`
	if spaceID != nil {
		cte += ` WHERE p.space_id = ` + qb.arg(*spaceID)
	}
	cte += `
		   AND pc.embedding IS NOT NULL
		 GROUP BY pc.page_id, p.space_id, p.title
		)`
	md := qb.arg(maxDist)
	lim := qb.arg(limit)
	q := `WITH ` + cte + `
		SELECT a.page_id, a.title, a.space_id, b.page_id, b.title, b.space_id,
		       1 - (a.c <=> b.c) AS sim
		  FROM cent a
		  JOIN cent b ON a.page_id < b.page_id
		 WHERE (a.c <=> b.c) <= ` + md + `
		 ORDER BY a.c <=> b.c ASC
		 LIMIT ` + lim

	rows, err := s.db.QueryContext(ctx, q, qb.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OverlapPair{}
	for rows.Next() {
		var p OverlapPair
		if err := rows.Scan(&p.PageA, &p.TitleA, &p.SpaceA, &p.PageB, &p.TitleB, &p.SpaceB, &p.Similarity); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
