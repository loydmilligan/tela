package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
)

const searchLimit = 25

type searchHit struct {
	PageID     int64    `json:"page_id"`
	SpaceID    int64    `json:"space_id"`
	Title      string   `json:"title"`
	Snippet    string   `json:"snippet"`
	Breadcrumb []string `json:"breadcrumb"`
}

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

	fts := buildFTSPhrasePrefix(q)

	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT p.id, p.space_id, p.title,
		       snippet(pages_fts, 1, '<mark>', '</mark>', '…', 32)
		FROM pages_fts
		JOIN pages p ON p.id = pages_fts.rowid
		JOIN space_members sm ON sm.space_id = p.space_id AND sm.user_id = ?
		WHERE pages_fts MATCH ?
		ORDER BY bm25(pages_fts) ASC
		LIMIT ?`, u.ID, fts, searchLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "search query failed")
		return
	}
	defer rows.Close()

	type hitRow struct {
		ID, SpaceID    int64
		Title, Snippet string
	}
	hits := []hitRow{}
	for rows.Next() {
		var h hitRow
		if err := rows.Scan(&h.ID, &h.SpaceID, &h.Title, &h.Snippet); err != nil {
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
			Snippet:    h.Snippet,
			Breadcrumb: bc,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// buildFTSPhrasePrefix wraps user input as a single FTS5 phrase-prefix search:
// double-quote the whole thing (so reserved chars and operators are inert),
// double internal quotes per FTS5 string-literal rules, then append `*` to
// match any token starting with the final word.
func buildFTSPhrasePrefix(q string) string {
	return `"` + strings.ReplaceAll(q, `"`, `""`) + `"*`
}

// pageBreadcrumb returns ancestor titles for pageID, ordered root → immediate
// parent. The page's own title is excluded (it's already on the hit). Empty
// slice for root pages.
func pageBreadcrumb(ctx context.Context, db *sql.DB, pageID int64) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		WITH RECURSIVE ancestors(id, parent_id, title, depth) AS (
		  SELECT id, parent_id, title, 0 FROM pages
		    WHERE id = (SELECT parent_id FROM pages WHERE id = ?)
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
