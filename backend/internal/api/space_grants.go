package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
)

// spaceGrantDTO is the wire shape for a space's org grants. Keyed by grant id
// so the principal kind stays an implementation detail (groups slot in later
// without a new route shape).
type spaceGrantDTO struct {
	ID        int64  `json:"id"`
	OrgID     int64  `json:"org_id"`
	OrgName   string `json:"org_name"`
	OrgSlug   string `json:"org_slug"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type spaceGrantAddRequest struct {
	OrgID int64  `json:"org_id"`
	Role  string `json:"role"`
}

type spaceGrantPatchRequest struct {
	Role string `json:"role"`
}

// grantRoleValid restricts org grants to editor/viewer. 'owner' is reserved for
// direct user members so the last-owner safeguard (which counts space_members)
// stays sound — a whole org can't become co-owner of a space.
func grantRoleValid(role string) bool {
	return role == roleEditor || role == roleViewer
}

// ListSpaceGrants returns the org grants on a space. Any space member can read.
func (s *Server) ListSpaceGrants(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := s.requireMembership(w, r, spaceID); !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT sg.id, o.id, o.name, o.slug, sg.role, sg.created_at, sg.updated_at
		  FROM space_grants sg
		  JOIN orgs o ON o.id = sg.principal_id
		 WHERE sg.space_id = ? AND sg.principal_kind = 'org'
		 ORDER BY o.name ASC`, spaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list space grants failed")
		return
	}
	defer rows.Close()

	grants := []spaceGrantDTO{}
	for rows.Next() {
		var g spaceGrantDTO
		if err := rows.Scan(&g.ID, &g.OrgID, &g.OrgName, &g.OrgSlug, &g.Role, &g.CreatedAt, &g.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan grant row failed")
			return
		}
		grants = append(grants, g)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate grants failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

// AddSpaceGrant shares a space with an org at editor/viewer. Space owner only.
func (s *Server) AddSpaceGrant(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	role, ok := s.requireMembership(w, r, spaceID)
	if !ok {
		return
	}
	if role != roleOwner {
		writeError(w, http.StatusForbidden, "forbidden", "owner role required")
		return
	}
	var req spaceGrantAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if req.OrgID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "org_id is required")
		return
	}
	if !grantRoleValid(req.Role) {
		writeError(w, http.StatusBadRequest, "bad_request", "role must be one of editor, viewer")
		return
	}

	ctx := r.Context()
	if exists, err := orgExists(ctx, s.DB, req.OrgID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup org failed")
		return
	} else if !exists {
		writeError(w, http.StatusNotFound, "not_found", "org not found")
		return
	}

	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO space_grants (space_id, principal_kind, principal_id, role)
		VALUES (?, 'org', ?, ?)`, spaceID, req.OrgID, req.Role)
	if err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "conflict", "this org already has a grant on the space")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "add space grant failed")
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "add grant: last insert id failed")
		return
	}
	dto, err := selectSpaceGrant(ctx, s.DB, spaceID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch added grant failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"grant": dto})
}

// PatchSpaceGrant changes a grant's role. Space owner only.
func (s *Server) PatchSpaceGrant(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	grantID, ok := parseIDParam(w, r, "grant_id")
	if !ok {
		return
	}
	role, ok := s.requireMembership(w, r, spaceID)
	if !ok {
		return
	}
	if role != roleOwner {
		writeError(w, http.StatusForbidden, "forbidden", "owner role required")
		return
	}
	var req spaceGrantPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if !grantRoleValid(req.Role) {
		writeError(w, http.StatusBadRequest, "bad_request", "role must be one of editor, viewer")
		return
	}

	ctx := r.Context()
	res, err := s.DB.ExecContext(ctx, `
		UPDATE space_grants SET role = ?, updated_at = datetime('now')
		 WHERE id = ? AND space_id = ? AND principal_kind = 'org'`, req.Role, grantID, spaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "update grant failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "grant not found")
		return
	}
	dto, err := selectSpaceGrant(ctx, s.DB, spaceID, grantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch updated grant failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grant": dto})
}

// DeleteSpaceGrant revokes an org's access to a space. Space owner only.
func (s *Server) DeleteSpaceGrant(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	grantID, ok := parseIDParam(w, r, "grant_id")
	if !ok {
		return
	}
	role, ok := s.requireMembership(w, r, spaceID)
	if !ok {
		return
	}
	if role != roleOwner {
		writeError(w, http.StatusForbidden, "forbidden", "owner role required")
		return
	}
	res, err := s.DB.ExecContext(r.Context(),
		`DELETE FROM space_grants WHERE id = ? AND space_id = ? AND principal_kind = 'org'`, grantID, spaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete grant failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "grant not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func selectSpaceGrant(ctx context.Context, db *sql.DB, spaceID, grantID int64) (spaceGrantDTO, error) {
	var g spaceGrantDTO
	err := db.QueryRowContext(ctx, `
		SELECT sg.id, o.id, o.name, o.slug, sg.role, sg.created_at, sg.updated_at
		  FROM space_grants sg
		  JOIN orgs o ON o.id = sg.principal_id
		 WHERE sg.id = ? AND sg.space_id = ? AND sg.principal_kind = 'org'`, grantID, spaceID).
		Scan(&g.ID, &g.OrgID, &g.OrgName, &g.OrgSlug, &g.Role, &g.CreatedAt, &g.UpdatedAt)
	return g, err
}
