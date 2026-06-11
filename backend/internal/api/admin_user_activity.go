package api

import (
	"database/sql"
	"net/http"
	"strings"
)

// admin_user_activity.go — GET /api/admin/users/{id}/activity. Instance-admin
// view of one user's recent edits across the WHOLE instance.
//
// Sibling of recent_changes.go: same page_revisions source, same one-row-per-page
// collapse, same recentChangeItem shape. The one deliberate difference is the
// access model — the home feed gates every row through the caller's space_access
// (you only see edits on pages you can open), whereas this admin view drops that
// gate so an instance-admin sees the target user's activity everywhere. That
// privilege is exactly why it's behind requireInstanceAdmin.

// ListUserActivity returns the target user's most recently edited pages (latest
// edit per page, newest first), instance-wide. ?source=agent|human filters by
// edit origin, mirroring the home feed. Instance-admin only.
func (s *Server) ListUserActivity(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	userID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	limit := clampLimit(r.URL.Query().Get("limit"), 20, 50)

	conds := []string{"pr.author_id = $1"}
	switch r.URL.Query().Get("source") {
	case "agent":
		conds = append(conds, "pr.source = 'agent'")
	case "human":
		conds = append(conds, "pr.source <> 'agent'")
	}
	where := "WHERE " + strings.Join(conds, " AND ")

	// No space_access join — the admin view is intentionally instance-wide (see
	// the file header). Collapse a page's many revisions to its newest one, then
	// order pages by recency.
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT t.page_id, t.title, t.space_id, t.space_name, t.username, t.created_at
		  FROM (
		    SELECT DISTINCT ON (p.id)
		           p.id AS page_id, p.title, p.space_id, sp.name AS space_name,
		           u.username, pr.created_at
		      FROM page_revisions pr
		      JOIN pages p ON p.id = pr.page_id AND p.deleted_at IS NULL
		      JOIN spaces sp ON sp.id = p.space_id
		      LEFT JOIN users u ON u.id = pr.author_id
		      `+where+`
		     ORDER BY p.id, pr.created_at DESC
		  ) t
		 ORDER BY t.created_at DESC
		 LIMIT $2`, userID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list user activity failed")
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
			writeError(w, http.StatusInternalServerError, "internal", "scan user activity row failed")
			return
		}
		it.AuthorUsername = nullableString(username)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate user activity failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": items})
}
