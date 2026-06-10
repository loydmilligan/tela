package api

import (
	"net/http"

	"github.com/zcag/tela/backend/internal/auth"
)

// Per-user pinned spaces. A pin is just a (user, space) edge that floats a space
// into a "Pinned" group at the top of the sidebar; visibility is still governed
// by space_access, so the list read re-gates through it and pinning requires at
// least viewer access. The list returns ids only — the frontend already holds the
// full Space objects from GET /api/spaces and partitions them by this set, so
// there's no second Space shape to drift. See migration 0032_pinned_spaces.sql.

// pinnedSpace is the wire shape for the pinned-spaces list: id + pin time (for
// ordering the Pinned group most-recent-first).
type pinnedSpace struct {
	SpaceID   int64  `json:"space_id"`
	CreatedAt string `json:"created_at"`
}

// ListPinnedSpaces returns the caller's pinned space ids, most-recent first,
// re-gated through space_access so a pin to a now-inaccessible space drops out.
func (s *Server) ListPinnedSpaces(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT ps.space_id, ps.created_at
		  FROM pinned_spaces ps
		  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sa
		    ON sa.space_id = ps.space_id
		 WHERE ps.user_id = $1
		 ORDER BY ps.created_at DESC`, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list pinned spaces failed")
		return
	}
	defer rows.Close()

	items := []pinnedSpace{}
	for rows.Next() {
		var it pinnedSpace
		if err := rows.Scan(&it.SpaceID, &it.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan pinned space row failed")
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate pinned spaces failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pinned_spaces": items})
}

// AddPinnedSpace pins a space for the caller. Requires viewer+ access; idempotent
// (re-pinning is a no-op).
func (s *Server) AddPinnedSpace(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	if _, ae := s.membershipCore(r.Context(), u, k, id); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO pinned_spaces (user_id, space_id) VALUES ($1, $2)
		 ON CONFLICT (user_id, space_id) DO NOTHING`, u.ID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "pin space failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"is_pinned": true})
}

// DeletePinnedSpace unpins a space for the caller. Idempotent — unpinning a space
// that isn't pinned (or no longer exists) still returns 204.
func (s *Server) DeletePinnedSpace(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`DELETE FROM pinned_spaces WHERE user_id = $1 AND space_id = $2`, u.ID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "unpin space failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
