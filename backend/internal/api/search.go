package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

const searchLimit = 25

// headlineOpts configures ts_headline: one <mark>-wrapped fragment, ~8-24 words,
// matching the snippet contract the frontend renders.
const headlineOpts = `StartSel=<mark>, StopSel=</mark>, MaxFragments=1, MaxWords=24, MinWords=8, ShortWord=2`

// stripExcalidrawSQL is the in-SQL Excalidraw-fence strip (mirrors the Go
// rag.StripExcalidrawFences) applied before ts_headline so snippets never show
// drawing JSON. The body fed to search_tsv (migration 0004) is stripped the
// same way, so ranking and snippeting agree.
const stripExcalidrawSQL = "regexp_replace(p.body, '```excalidraw.*?```', '', 'g')"

type searchHit struct {
	PageID     int64    `json:"page_id"`
	SpaceID    int64    `json:"space_id"`
	Title      string   `json:"title"`
	Snippet    string   `json:"snippet"`
	Breadcrumb []string `json:"breadcrumb"`
}

// Search is the Tier-2 server-side full-text search behind the command palette.
//
// Ranked Postgres FTS over pages.search_tsv (migration 0004): title weighted
// above body, Excalidraw stripped, ordered by ts_rank_cd, snippet via
// ts_headline. websearch_to_tsquery parses the user's raw input forgivingly
// (quotes/operators tolerated, never errors) so the box can't 500 on
// punctuation. Scoped through space_access. The instant client-side tiers (Orama
// titles + bodies) paint first; this fills in ranked hits on the debounce.
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

	// Bearer-mode with a space_id restriction narrows to that one space — without
	// it we'd surface titles from any space the user is a member of, even though
	// the bearer scope forbids opening them.
	var (
		rows *sql.Rows
		err  error
	)
	if k, isBearer := auth.APIKeyFromContext(r.Context()); isBearer && k.SpaceID != nil {
		rows, err = s.DB.QueryContext(r.Context(), `
			SELECT p.id, p.space_id, p.title,
			       ts_headline('english', `+stripExcalidrawSQL+`,
			                   websearch_to_tsquery('english', $3), $4) AS snippet
			FROM pages p
			JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm ON sm.space_id = p.space_id
			WHERE p.space_id = $2 AND p.search_tsv @@ websearch_to_tsquery('english', $3)
			ORDER BY ts_rank_cd(p.search_tsv, websearch_to_tsquery('english', $3)) DESC, p.updated_at DESC
			LIMIT $5`, u.ID, *k.SpaceID, q, headlineOpts, searchLimit)
	} else {
		rows, err = s.DB.QueryContext(r.Context(), `
			SELECT p.id, p.space_id, p.title,
			       ts_headline('english', `+stripExcalidrawSQL+`,
			                   websearch_to_tsquery('english', $2), $3) AS snippet
			FROM pages p
			JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm ON sm.space_id = p.space_id
			WHERE p.search_tsv @@ websearch_to_tsquery('english', $2)
			ORDER BY ts_rank_cd(p.search_tsv, websearch_to_tsquery('english', $2)) DESC, p.updated_at DESC
			LIMIT $4`, u.ID, q, headlineOpts, searchLimit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "search query failed")
		return
	}
	defer rows.Close()

	type hitRow struct {
		ID, SpaceID int64
		Title       string
		Snippet     string
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
