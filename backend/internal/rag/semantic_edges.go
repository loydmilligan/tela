package rag

import "context"

// Semantic graph edges — the "what's actually about the same thing" layer for the
// graph view. Where page_links/tree edges are connections a human drew, these are
// computed from the chunk embeddings already in the DB (no live embedder): each
// page linked to its K nearest neighbours by centroid cosine similarity, above a
// floor. Overlaid on the authored graph, a semantic edge with no link underneath
// is a visible structural hole — two pages clearly related that nobody connected.
//
// kNN-per-node (LATERAL) rather than all-pairs-over-threshold so every page gets
// a few neighbours instead of the result being swamped by one dense cluster.
// Centroid-per-page keeps it an n²-ish scan that stays cheap at wiki scale.

// SemanticEdge is an undirected similarity edge between two pages.
type SemanticEdge struct {
	Source     int64   `json:"source"`
	Target     int64   `json:"target"`
	Similarity float64 `json:"similarity"` // cosine similarity of centroids, [0,1]
}

// SemanticEdges returns up to k nearest neighbours per page (deduped to undirected
// edges, strongest similarity kept), access-scoped through space_access. spaceID,
// when non-nil, restricts to edges WITHIN one space. Uses stored vectors only — no
// live embedder — so it works whenever pages have been indexed. k defaults to 5,
// threshold to 0.55, limit (a safety cap on raw kNN rows) to 2000.
func (s *Service) SemanticEdges(ctx context.Context, userID int64, spaceID *int64, k int, threshold float64, limit int) ([]SemanticEdge, error) {
	if k <= 0 || k > 12 {
		k = 5
	}
	if threshold <= 0 || threshold > 1 {
		threshold = 0.55
	}
	if limit <= 0 || limit > 8000 {
		limit = 2000
	}
	maxDist := 1 - threshold

	qb := &queryBuilder{}
	uid := qb.arg(userID)
	cte := `
		cent AS (
		  SELECT pc.page_id, p.space_id, avg(pc.embedding) AS c
		    FROM page_chunks pc
		    JOIN pages p ON p.id = pc.page_id AND p.deleted_at IS NULL
		    JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = ` + uid + `) sm
		      ON sm.space_id = p.space_id`
	if spaceID != nil {
		cte += ` WHERE p.space_id = ` + qb.arg(*spaceID)
	}
	cte += `
		   AND pc.embedding IS NOT NULL
		 GROUP BY pc.page_id, p.space_id
		)`
	md := qb.arg(maxDist)
	kArg := qb.arg(k)
	lim := qb.arg(limit)
	// For each page a, its k closest other pages within the distance floor.
	q := `WITH ` + cte + `
		SELECT a.page_id, nb.page_id, 1 - (a.c <=> nb.c) AS sim
		  FROM cent a
		  JOIN LATERAL (
		    SELECT b.page_id, b.c
		      FROM cent b
		     WHERE b.page_id <> a.page_id AND (a.c <=> b.c) <= ` + md + `
		     ORDER BY a.c <=> b.c ASC
		     LIMIT ` + kArg + `
		  ) nb ON true
		 LIMIT ` + lim

	rows, err := s.db.QueryContext(ctx, q, qb.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Dedup to undirected edges: a→b and b→a collapse to one (lo,hi), keeping the
	// higher similarity (the two directions can differ — kNN isn't symmetric).
	type pair struct{ lo, hi int64 }
	best := map[pair]float64{}
	for rows.Next() {
		var src, tgt int64
		var sim float64
		if err := rows.Scan(&src, &tgt, &sim); err != nil {
			return nil, err
		}
		p := pair{src, tgt}
		if src > tgt {
			p = pair{tgt, src}
		}
		if cur, ok := best[p]; !ok || sim > cur {
			best[p] = sim
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]SemanticEdge, 0, len(best))
	for p, sim := range best {
		out = append(out, SemanticEdge{Source: p.lo, Target: p.hi, Similarity: sim})
	}
	return out, nil
}
