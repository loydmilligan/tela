package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

// M16.A.5 server-side body search, used by the MCP `search_bodies` tool.
//
// Endpoint: GET /api/search/bodies?space_id={id}&q={query}&limit={1..100}
// Auth: session cookie OR bearer-`read` (middleware enforces). Membership in
// space_id required (viewer-OK).
//
// Ranked Postgres FTS over pages.search_tsv (migration 0004), scoped to one
// space. score = ts_rank_cd (higher = better), replacing the old FTS5 bm25.
// websearch_to_tsquery parses raw input forgivingly so odd punctuation can't
// 500 the MCP tool's search loop.

const (
	searchBodiesDefaultLimit = 20
	searchBodiesMinLimit     = 1
	searchBodiesMaxLimit     = 100
)

type searchBodyHit struct {
	ID    int64   `json:"id"`
	Title string  `json:"title"`
	Score float64 `json:"score"`
}

func (s *Server) SearchBodies(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	spaceIDStr := q.Get("space_id")
	if spaceIDStr == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "space_id is required")
		return
	}
	spaceID, err := strconv.ParseInt(spaceIDStr, 10, 64)
	if err != nil || spaceID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "space_id must be a positive integer")
		return
	}

	limit := 0
	if raw := q.Get("limit"); raw != "" {
		if v, perr := strconv.Atoi(raw); perr == nil {
			limit = v
		}
	}

	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	results, ae := s.searchBodiesCore(r.Context(), u, k, spaceID, q.Get("q"), limit)
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// searchBodiesCore is the transport-agnostic core behind GET /api/search/bodies
// and the MCP search_bodies tool: ranked FTS within one space, viewer-OK. limit
// clamps silently to [1,100] (≤0 → default 20) per the M16.A.5 contract so an
// agent passing a wild limit gets a sane result set instead of an error.
func (s *Server) searchBodiesCore(ctx context.Context, u *auth.User, k *auth.APIKey, spaceID int64, rawQuery string, limit int) ([]searchBodyHit, *apiErr) {
	rawQuery = strings.TrimSpace(rawQuery)
	if rawQuery == "" {
		return nil, &apiErr{http.StatusBadRequest, "invalid_query", "q is required"}
	}
	if limit <= 0 {
		limit = searchBodiesDefaultLimit
	}
	if limit < searchBodiesMinLimit {
		limit = searchBodiesMinLimit
	}
	if limit > searchBodiesMaxLimit {
		limit = searchBodiesMaxLimit
	}

	if err := verifySpaceExists(ctx, s.DB, spaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &apiErr{http.StatusNotFound, "space_not_found", "space not found"}
		}
		return nil, &apiErr{http.StatusInternalServerError, "internal", "lookup space failed"}
	}
	if _, ae := s.membershipCore(ctx, u, k, spaceID); ae != nil {
		return nil, ae
	}

	rows, err := s.DB.QueryContext(ctx, `
		SELECT p.id, p.title,
		       ts_rank_cd(p.search_tsv, websearch_to_tsquery('english', $2)) AS score
		  FROM pages p
		 WHERE p.space_id = $1
		   AND p.deleted_at IS NULL
		   AND p.search_tsv @@ websearch_to_tsquery('english', $2)
		 ORDER BY score DESC, p.updated_at DESC
		 LIMIT $3`, spaceID, rawQuery, limit)
	if err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "search query failed"}
	}
	defer rows.Close()

	results := make([]searchBodyHit, 0, limit)
	for rows.Next() {
		var (
			id    int64
			title string
			score float64
		)
		if err := rows.Scan(&id, &title, &score); err != nil {
			return nil, &apiErr{http.StatusInternalServerError, "internal", "scan search row failed"}
		}
		results = append(results, searchBodyHit{ID: id, Title: title, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "iterate search rows failed"}
	}
	return results, nil
}
