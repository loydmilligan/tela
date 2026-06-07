package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/zcag/tela/backend/internal/auth"
)

// Per-user page favorites. A favorite is just a (user, page) edge; visibility is
// still governed by space_access, so the list read re-gates through it and
// starring requires at least viewer access to the page's space. See
// migration 0005_favorites.sql.

// favoriteItem is the wire shape for the favorites list — enough to render a
// sidebar / dashboard row and navigate to the page.
type favoriteItem struct {
	PageID    int64  `json:"page_id"`
	Title     string `json:"title"`
	SpaceID   int64  `json:"space_id"`
	SpaceName string `json:"space_name"`
	CreatedAt string `json:"created_at"`
}

// pageSpaceID resolves the space a page belongs to. Returns sql.ErrNoRows when
// the page doesn't exist.
func pageSpaceID(r *http.Request, db *sql.DB, pageID int64) (int64, error) {
	var spaceID int64
	err := db.QueryRowContext(r.Context(),
		`SELECT space_id FROM pages WHERE id = $1 AND deleted_at IS NULL`, pageID).Scan(&spaceID)
	return spaceID, err
}

// ListFavorites returns the caller's starred pages, most-recent first, re-gated
// through space_access so a favorite to a now-inaccessible page drops out.
func (s *Server) ListFavorites(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT f.page_id, p.title, p.space_id, sp.name, f.created_at
		  FROM favorites f
		  JOIN pages p ON p.id = f.page_id AND p.deleted_at IS NULL
		  JOIN spaces sp ON sp.id = p.space_id
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sa
		    ON sa.space_id = p.space_id
		 WHERE f.user_id = $1
		 ORDER BY f.created_at DESC`, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list favorites failed")
		return
	}
	defer rows.Close()

	items := []favoriteItem{}
	for rows.Next() {
		var it favoriteItem
		if err := rows.Scan(&it.PageID, &it.Title, &it.SpaceID, &it.SpaceName, &it.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan favorite row failed")
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate favorites failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"favorites": items})
}

// GetFavoriteStatus reports whether the caller has starred a page.
func (s *Server) GetFavoriteStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var exists int
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT 1 FROM favorites WHERE user_id = $1 AND page_id = $2`, u.ID, id).Scan(&exists)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "internal", "lookup favorite failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"is_favorited": err == nil})
}

// AddFavorite stars a page for the caller. Requires viewer+ access to the page's
// space; idempotent (re-starring is a no-op).
func (s *Server) AddFavorite(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	spaceID, err := pageSpaceID(r, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if _, ae := s.membershipCore(r.Context(), u, k, spaceID); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO favorites (user_id, page_id) VALUES ($1, $2)
		 ON CONFLICT (user_id, page_id) DO NOTHING`, u.ID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "add favorite failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"is_favorited": true})
}

// DeleteFavorite unstars a page for the caller. Idempotent — unstarring a page
// that isn't starred (or no longer exists) still returns 204.
func (s *Server) DeleteFavorite(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`DELETE FROM favorites WHERE user_id = $1 AND page_id = $2`, u.ID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "remove favorite failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
