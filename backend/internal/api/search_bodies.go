package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// M16.A.5 server-side body-fuzzy search. Powers the MCP `search_bodies` tool
// without paying the 5–10 s Orama cold-start the FE BodyIndex would otherwise
// re-incur on every stdio spawn.
//
// Endpoint: GET /api/search/bodies?space_id={id}&q={query}&limit={1..100}
// Auth: session cookie OR bearer-`read` (middleware enforces). Membership in
// space_id required (viewer-OK). The optional bearer space-restriction is
// enforced by requireMembership → enforceAPIKeySpaceScope before any DB read.
//
// Scoring: pages_fts already strips Excalidraw fences from `body` at
// index-write time (see migration 0010_fts_strip_excalidraw.sql), so the
// MATCH never surfaces drawing JSON. The FTS5 row indexes both title and
// body, so a title-only match still counts — matches the MCP `search_bodies`
// contract which doesn't promise body-only matching.
//
// FTS5's bm25() returns NEGATIVE values where more-negative = better match,
// so the API exposes a [0,1) normalisation `-bm25 / (1 - bm25)` (sigmoid-like
// in shape, no divide-by-zero, no sign confusion). The literal `1/(1+bm25)`
// formula from the design doc is non-monotonic for FTS5's negative sign
// convention; we deviate to make consumers see higher = better as advertised.

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
	// limit shouldn't break their search loop, just get a reasonable result
	// set back.
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
	// requireMembership writes the 403 envelope on non-member (forbidden) or
	// bearer-space-mismatch (api_key_space_scope) and returns ok=false.
	if _, ok := s.requireMembership(w, r, spaceID); !ok {
		return
	}

	fts := buildFTSBodyMatch(rawQuery)
	if fts == "" {
		// All terms stripped by the sanitiser (e.g., q='+"-*'); not a 500.
		writeJSON(w, http.StatusOK, map[string]any{"results": []searchBodyHit{}})
		return
	}

	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT p.id, p.title, bm25(pages_fts)
		  FROM pages_fts
		  JOIN pages p ON p.id = pages_fts.rowid
		 WHERE pages_fts MATCH ?
		   AND p.space_id = ?
		 ORDER BY bm25(pages_fts) ASC, p.updated_at DESC
		 LIMIT ?`, fts, spaceID, limit)
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
			raw   float64
		)
		if err := rows.Scan(&id, &title, &raw); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan search row failed")
			return
		}
		results = append(results, searchBodyHit{
			ID:    id,
			Title: title,
			Score: normalizeBM25(raw),
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate search rows failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// buildFTSBodyMatch builds a per-term phrase-prefix MATCH expression. Each
// whitespace-separated term in q is double-quote-wrapped and suffixed with
// `*`, then joined with spaces. FTS5's implicit AND across whitespace-
// separated tokens means every term must match somewhere in the document.
//
// The quote-wrap doubles internal `"` per FTS5 string-literal rules; the rest
// of FTS5's syntax characters become literal text inside the phrase, so an
// injection like `q='+evil-stuff'` reduces to a plain prefix-matched phrase
// rather than a syntactically broken MATCH that would 500. We also strip the
// `*` character explicitly so a trailing user-supplied wildcard cannot stack
// with the appended one (FTS5 tolerates `**` but it's defensive).
//
// Returns "" when every term collapses to empty after sanitisation — callers
// handle that as a 200 empty result rather than feeding `MATCH ''` to the
// engine (which is undefined behaviour in FTS5).
func buildFTSBodyMatch(q string) string {
	var terms []string
	for _, raw := range strings.Fields(q) {
		cleaned := strings.Map(func(r rune) rune {
			if r == '*' {
				return -1
			}
			return r
		}, raw)
		cleaned = strings.ReplaceAll(cleaned, `"`, `""`)
		if cleaned == "" {
			continue
		}
		terms = append(terms, `"`+cleaned+`"*`)
	}
	return strings.Join(terms, " ")
}

// normalizeBM25 maps FTS5's bm25() output (NEGATIVE, more-negative = better
// match) onto [0, 1) with higher = better. Uses `-x / (1 - x)`:
//   - bm25 =   0  →  0.0   (no match contribution — shouldn't appear in MATCH'd rows)
//   - bm25 =  -1  →  0.5
//   - bm25 = -10  →  0.909
//   - bm25 = -20  →  0.952
//
// Bijective from (-∞, 0] to [0, 1); no division-by-zero across the FTS5 range.
func normalizeBM25(raw float64) float64 {
	if raw >= 0 {
		// FTS5 doesn't produce positive bm25 in practice, but clamp defensively.
		return 0
	}
	return -raw / (1.0 - raw)
}
