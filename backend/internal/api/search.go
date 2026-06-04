package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

const searchLimit = 25

type searchHit struct {
	PageID     int64    `json:"page_id"`
	SpaceID    int64    `json:"space_id"`
	Title      string   `json:"title"`
	Snippet    string   `json:"snippet"`
	Breadcrumb []string `json:"breadcrumb"`
}

// TODO(search): PLACEHOLDER implementation.
//
// The SQLite FTS5 search (MATCH / bm25 ranking / snippet() / the
// tela_strip_excalidraw index UDF) was removed in the Postgres switch. This is
// a deliberately dumb ILIKE substring scan over title+body — correct and
// access-controlled, but unranked and not "instant". It exists only so the
// search box keeps working during the migration.
//
// The real design — a zero-network client-side instant tier (Orama) layered
// with server-side semantic refinement (pgvector) streaming in over a debounce,
// plus tsvector/pg_trgm for the lexical server tier — is captured in
// docs/search.md. All three (tsvector, pg_trgm, pgvector) live in this same
// Postgres, so the rebuild is additive: a future migration + a rewrite of the
// two queries below, no new infra.
func (s *Server) Search(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"results": []searchHit{}})
		return
	}

	pattern := "%" + escapeLike(q) + "%"

	// Bearer-mode with a space_id restriction narrows the scan to that one space
	// — without it we'd surface titles from any space the user is a member of,
	// even though the bearer scope forbids opening them.
	var (
		rows *sql.Rows
		err  error
	)
	if k, isBearer := auth.APIKeyFromContext(r.Context()); isBearer && k.SpaceID != nil {
		rows, err = s.DB.QueryContext(r.Context(), `
			SELECT p.id, p.space_id, p.title, p.body
			FROM pages p
			JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm ON sm.space_id = p.space_id
			WHERE p.space_id = $2 AND (p.title ILIKE $3 ESCAPE '\' OR p.body ILIKE $3 ESCAPE '\')
			ORDER BY p.updated_at DESC
			LIMIT $4`, u.ID, *k.SpaceID, pattern, searchLimit)
	} else {
		rows, err = s.DB.QueryContext(r.Context(), `
			SELECT p.id, p.space_id, p.title, p.body
			FROM pages p
			JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm ON sm.space_id = p.space_id
			WHERE p.title ILIKE $2 ESCAPE '\' OR p.body ILIKE $2 ESCAPE '\'
			ORDER BY p.updated_at DESC
			LIMIT $3`, u.ID, pattern, searchLimit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "search query failed")
		return
	}
	defer rows.Close()

	type hitRow struct {
		ID, SpaceID int64
		Title, Body string
	}
	hits := []hitRow{}
	for rows.Next() {
		var h hitRow
		if err := rows.Scan(&h.ID, &h.SpaceID, &h.Title, &h.Body); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan search row failed")
			return
		}
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate search rows failed")
		return
	}

	results := make([]searchHit, 0, len(hits))
	for _, h := range hits {
		bc, err := pageBreadcrumb(r.Context(), s.DB, h.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "build breadcrumb failed")
			return
		}
		results = append(results, searchHit{
			PageID:     h.ID,
			SpaceID:    h.SpaceID,
			Title:      h.Title,
			Snippet:    makeSnippet(h.Body, q),
			Breadcrumb: bc,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// escapeLike escapes the LIKE/ILIKE wildcard metacharacters in user input so a
// query containing % or _ is matched literally. Pairs with `ESCAPE '\'` in the
// query. The backslash itself is escaped first so it can't form an escape pair
// with following user text.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

// makeSnippet returns a short context window around the first case-insensitive
// occurrence of q in body, with the match wrapped in <mark>…</mark> to mirror
// the old FTS snippet() contract the frontend renders. Falls back to the head
// of the body when q isn't found in it (a title-only match).
func makeSnippet(body, q string) string {
	const window = 32
	idx := strings.Index(strings.ToLower(body), strings.ToLower(q))
	if idx < 0 {
		if len(body) > window*2 {
			return strings.TrimSpace(body[:window*2]) + "…"
		}
		return body
	}
	start := idx - window
	prefix := ""
	if start < 0 {
		start = 0
	} else {
		prefix = "…"
	}
	end := idx + len(q) + window
	suffix := ""
	if end > len(body) {
		end = len(body)
	} else {
		suffix = "…"
	}
	matchEnd := idx + len(q)
	return prefix + body[start:idx] + "<mark>" + body[idx:matchEnd] + "</mark>" + body[matchEnd:end] + suffix
}

// pageBreadcrumb returns ancestor titles for pageID, ordered root → immediate
// parent. The page's own title is excluded (it's already on the hit). Empty
// slice for root pages.
func pageBreadcrumb(ctx context.Context, db *sql.DB, pageID int64) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		WITH RECURSIVE ancestors(id, parent_id, title, depth) AS (
		  SELECT id, parent_id, title, 0 FROM pages
		    WHERE id = (SELECT parent_id FROM pages WHERE id = $1)
		  UNION ALL
		  SELECT p.id, p.parent_id, p.title, a.depth + 1
		    FROM ancestors a JOIN pages p ON p.id = a.parent_id
		)
		SELECT title FROM ancestors ORDER BY depth DESC`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	titles := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		titles = append(titles, t)
	}
	return titles, rows.Err()
}
