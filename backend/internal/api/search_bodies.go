package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// M16.A.5 server-side body search, used by the MCP `search_bodies` tool.
//
// Endpoint: GET /api/search/bodies?space_id={id}&q={query}&limit={1..100}
// Auth: session cookie OR bearer-`read` (middleware enforces). Membership in
// space_id required (viewer-OK).
//
// TODO(search): PLACEHOLDER — see the banner on Search() in search.go. This was
// SQLite FTS5 bm25-ranked body search; it's now an unranked ILIKE substring
// scan over title+body so the MCP tool keeps returning sane results during the
// Postgres migration. Score is a constant 1.0 (no ranking yet). The real
// ranked/semantic design lives in docs/search.md.

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

	rawQuery := strings.TrimSpace(q.Get("q"))
	if rawQuery == "" {
		writeError(w, http.StatusBadRequest, "invalid_query", "q is required")
		return
	}

	// Limit clamps silently per the M16.A.5 contract — agents passing a wild
	// limit shouldn't break their search loop, just get a reasonable result set.
	limit := int64(searchBodiesDefaultLimit)
	if raw := q.Get("limit"); raw != "" {
		v, perr := strconv.ParseInt(raw, 10, 64)
		if perr == nil && v > 0 {
			limit = v
		}
	}
	if limit < searchBodiesMinLimit {
		limit = searchBodiesMinLimit
	}
	if limit > searchBodiesMaxLimit {
		limit = searchBodiesMaxLimit
	}

	if err := verifySpaceExists(r.Context(), s.DB, spaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "space_not_found", "space not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "lookup space failed")
		return
	}
	// requireMembership writes the 403 envelope on non-member or bearer-space
	// mismatch and returns ok=false.
	if _, ok := s.requireMembership(w, r, spaceID); !ok {
		return
	}

	pattern := "%" + escapeLike(rawQuery) + "%"
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT p.id, p.title
		  FROM pages p
		 WHERE p.space_id = $1
		   AND (p.title ILIKE $2 ESCAPE '\' OR p.body ILIKE $2 ESCAPE '\')
		 ORDER BY p.updated_at DESC
		 LIMIT $3`, spaceID, pattern, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "search query failed")
		return
	}
	defer rows.Close()

	results := make([]searchBodyHit, 0, limit)
	for rows.Next() {
		var (
			id    int64
			title string
		)
		if err := rows.Scan(&id, &title); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan search row failed")
			return
		}
		results = append(results, searchBodyHit{ID: id, Title: title, Score: 1.0})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate search rows failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}
