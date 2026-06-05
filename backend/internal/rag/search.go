package rag

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

// Hit is one ranked chunk result. Carries everything a caller needs to cite the
// source: page id + heading path (and the API layer adds the in-app URL).
// UpdatedAt surfaces freshness so an agent/UI can warn on stale facts.
type Hit struct {
	ChunkID     int64   `json:"chunk_id"`
	PageID      int64   `json:"page_id"`
	SpaceID     int64   `json:"space_id"`
	Title       string  `json:"title"`
	HeadingPath string  `json:"heading_path"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score"`
	UpdatedAt   string  `json:"updated_at"`
}

// rrfK is the standard Reciprocal Rank Fusion constant. Larger = flatter
// weighting across ranks; 60 is the well-trodden default and needs no score
// calibration between the lexical and vector rankers.
const rrfK = 60

// Search runs hybrid retrieval scoped to what userID can access (the
// space_access view). mode is "hybrid" (default), "semantic", or "lexical".
// spaceID, when non-nil, narrows to a single space.
func (s *Service) Search(ctx context.Context, userID int64, q string, spaceID *int64, limit int, mode string) ([]Hit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []Hit{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	// Pull a deeper candidate pool from each ranker than we return, so fusion
	// has room to reorder.
	pool := limit * 4
	if pool < 40 {
		pool = 40
	}

	var lex, vec []int64
	if mode != "semantic" {
		ids, err := s.lexicalRank(ctx, userID, q, spaceID, pool)
		if err != nil {
			return nil, err
		}
		lex = ids
	}
	if mode != "lexical" {
		ids, err := s.vectorRank(ctx, userID, q, spaceID, pool)
		if err != nil {
			return nil, err
		}
		vec = ids
	}

	// Reciprocal Rank Fusion: each list contributes 1/(k + rank) to a chunk's
	// score; chunks ranked highly by both rankers rise to the top.
	score := map[int64]float64{}
	order := []int64{}
	add := func(ids []int64) {
		for rank, id := range ids {
			if _, seen := score[id]; !seen {
				order = append(order, id)
			}
			score[id] += 1.0 / float64(rrfK+rank+1)
		}
	}
	add(lex)
	add(vec)

	sort.SliceStable(order, func(i, j int) bool { return score[order[i]] > score[order[j]] })
	if len(order) > limit {
		order = order[:limit]
	}
	return s.hydrate(ctx, order, score)
}

// lexicalRank returns chunk ids ranked by ts_rank_cd over the generated
// content_tsv, scoped by space_access (and optionally a single space). The
// permission join is against the LIVE pages row (p.space_id), never a chunk
// copy — the anti-leak invariant.
func (s *Service) lexicalRank(ctx context.Context, userID int64, q string, spaceID *int64, limit int) ([]int64, error) {
	qb := &queryBuilder{}
	uid := qb.arg(userID)
	qry := qb.arg(q)
	sql := `
		SELECT pc.id
		  FROM page_chunks pc
		  JOIN pages p ON p.id = pc.page_id
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = ` + uid + `) sm
		    ON sm.space_id = p.space_id
		 WHERE pc.content_tsv @@ plainto_tsquery('english', ` + qry + `)`
	if spaceID != nil {
		sql += ` AND p.space_id = ` + qb.arg(*spaceID)
	}
	sql += ` ORDER BY ts_rank_cd(pc.content_tsv, plainto_tsquery('english', ` + qry + `)) DESC LIMIT ` + qb.arg(limit)
	return s.queryIDs(ctx, sql, qb.args...)
}

// vectorRank embeds the query and returns chunk ids ranked by cosine distance
// (pgvector <=>, ascending = closest), scoped by space_access. No ANN index yet
// — an exact scan over the (small) permitted candidate set (see 0002_rag.sql).
func (s *Service) vectorRank(ctx context.Context, userID int64, q string, spaceID *int64, limit int) ([]int64, error) {
	vec, err := s.emb.Embed(ctx, q)
	if err != nil {
		return nil, err
	}
	qb := &queryBuilder{}
	uid := qb.arg(userID)
	qvec := qb.arg(vecLiteral(vec))
	sql := `
		SELECT pc.id
		  FROM page_chunks pc
		  JOIN pages p ON p.id = pc.page_id
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = ` + uid + `) sm
		    ON sm.space_id = p.space_id
		 WHERE pc.embedding IS NOT NULL`
	if spaceID != nil {
		sql += ` AND p.space_id = ` + qb.arg(*spaceID)
	}
	sql += ` ORDER BY pc.embedding <=> ` + qvec + `::vector LIMIT ` + qb.arg(limit)
	return s.queryIDs(ctx, sql, qb.args...)
}

func (s *Service) queryIDs(ctx context.Context, query string, args ...any) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
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

// hydrate fetches display fields for the fused id list and returns hits in fused
// order (the IN-clause result order is undefined, so we re-sort by score).
func (s *Service) hydrate(ctx context.Context, ids []int64, score map[int64]float64) ([]Hit, error) {
	if len(ids) == 0 {
		return []Hit{}, nil
	}
	qb := &queryBuilder{}
	ph := make([]string, len(ids))
	for i, id := range ids {
		ph[i] = qb.arg(id)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT pc.id, pc.page_id, p.space_id, pc.heading_path, pc.content, p.title, p.updated_at
		  FROM page_chunks pc
		  JOIN pages p ON p.id = pc.page_id
		 WHERE pc.id IN (`+strings.Join(ph, ",")+`)`, qb.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := make(map[int64]Hit, len(ids))
	for rows.Next() {
		var (
			h       Hit
			content string
		)
		if err := rows.Scan(&h.ChunkID, &h.PageID, &h.SpaceID, &h.HeadingPath, &content, &h.Title, &h.UpdatedAt); err != nil {
			return nil, err
		}
		h.Snippet = snippet(content, 280)
		h.Score = score[h.ChunkID]
		byID[h.ChunkID] = h
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]Hit, 0, len(ids))
	for _, id := range ids {
		if h, ok := byID[id]; ok {
			out = append(out, h)
		}
	}
	return out, nil
}

// queryBuilder accumulates positional args and hands back $1, $2, … placeholders
// in order — so optional WHERE clauses can be appended without hand-counting.
type queryBuilder struct {
	args []any
}

func (b *queryBuilder) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

// snippet returns a single-line preview of content, truncated to roughly n runes
// on a word boundary.
func snippet(content string, n int) string {
	content = strings.Join(strings.Fields(content), " ")
	if len(content) <= n {
		return content
	}
	cut := content[:n]
	if i := strings.LastIndexByte(cut, ' '); i > n/2 {
		cut = cut[:i]
	}
	return cut + "…"
}
