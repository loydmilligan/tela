package api

import (
	"database/sql"
	"net/http"
	"strings"
)

// Recent changes feed for the home dashboard: the latest edit per page across
// every space the caller can reach, newest first. Built from page_revisions
// (one snapshot per body/title change) — no new event source. Gated through
// space_access so it never leaks a page the user can't open.

// recentChangeItem is one row of the feed: a page plus who last touched it and
// when. AuthorUsername is nil for system/anonymous edits.
type recentChangeItem struct {
	PageID         int64   `json:"page_id"`
	Title          string  `json:"title"`
	SpaceID        int64   `json:"space_id"`
	SpaceName      string  `json:"space_name"`
	AuthorUsername *string `json:"author_username"`
	UpdatedAt      string  `json:"updated_at"`
}

// ListRecentChanges returns the most recently edited pages the caller can see.
// DISTINCT ON collapses a page's many revisions to its newest one; the outer
// sort then orders pages by recency. Filters (applied to the revisions before
// the collapse): ?mine=1 → only the caller's own edits; ?source=agent → only
// agent/MCP edits; ?source=human → only non-agent edits. The dashboard pairs
// mine+human ("My recent edits") with mine+agent ("Changes by your AI").
func (s *Server) ListRecentChanges(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	limit := clampLimit(r.URL.Query().Get("limit"), 20, 50)
	// Filters restrict which revisions count before collapsing to one row per
	// page — "pages you changed" / "pages your AI changed", not just "changed".
	conds := []string{}
	if r.URL.Query().Get("mine") == "1" {
		conds = append(conds, "pr.author_id = $1")
	}
	switch r.URL.Query().Get("source") {
	case "agent":
		conds = append(conds, "pr.source = 'agent'")
	case "human":
		conds = append(conds, "pr.source <> 'agent'")
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT t.page_id, t.title, t.space_id, t.space_name, t.username, t.created_at
		  FROM (
		    SELECT DISTINCT ON (p.id)
		           p.id AS page_id, p.title, p.space_id, sp.name AS space_name,
		           u.username, pr.created_at
		      FROM page_revisions pr
		      JOIN pages p ON p.id = pr.page_id
		      JOIN spaces sp ON sp.id = p.space_id
		      JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sa
		        ON sa.space_id = p.space_id
		      LEFT JOIN users u ON u.id = pr.author_id
		      `+where+`
		     ORDER BY p.id, pr.created_at DESC
		  ) t
		 ORDER BY t.created_at DESC
		 LIMIT $2`, u.ID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list recent changes failed")
		return
	}
	defer rows.Close()

	items := []recentChangeItem{}
	for rows.Next() {
		var (
			it       recentChangeItem
			username sql.NullString
		)
		if err := rows.Scan(&it.PageID, &it.Title, &it.SpaceID, &it.SpaceName, &username, &it.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan recent change row failed")
			return
		}
		it.AuthorUsername = nullableString(username)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate recent changes failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": items})
}
