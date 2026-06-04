package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
)

// Space grants share a space with a non-user principal (an org or a group), at
// editor/viewer. Keyed by grant id so the principal kind is an implementation
// detail of the row, not the route. Owner-gated; 'owner' is rejected (API +
// DB trigger) so the last-owner guard stays sound.

// spaceGrantDTO is principal-generic: PrincipalName is the org/group name;
// ContextName is the parent org's name for a group grant (nil for an org).
type spaceGrantDTO struct {
	ID            int64   `json:"id"`
	PrincipalKind string  `json:"principal_kind"` // "org" | "group"
	PrincipalID   int64   `json:"principal_id"`
	PrincipalName string  `json:"principal_name"`
	ContextName   *string `json:"context_name"`
	Role          string  `json:"role"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

type spaceGrantAddRequest struct {
	PrincipalKind string `json:"principal_kind"`
	PrincipalID   int64  `json:"principal_id"`
	Role          string `json:"role"`
}

type spaceGrantPatchRequest struct {
	Role string `json:"role"`
}

// grantRoleValid restricts grants to editor/viewer. 'owner' is reserved for
// direct user members (last-owner guard counts space_members).
func grantRoleValid(role string) bool {
	return role == roleEditor || role == roleViewer
}

func validPrincipalKind(kind string) bool {
	return kind == "org" || kind == "group"
}

// principalExists reports whether an org/group principal_id is real.
func principalExists(ctx context.Context, db *sql.DB, kind string, id int64) (bool, error) {
	var table string
	switch kind {
	case "org":
		table = "orgs"
	case "group":
		table = "groups"
	default:
		return false, nil
	}
	var x int
	err := db.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE id = $1", id).Scan(&x)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

const spaceGrantSelect = `
	SELECT sg.id, sg.principal_kind, sg.principal_id,
	       COALESCE(o.name, g.name) AS principal_name,
	       go.name AS context_name,
	       sg.role, sg.created_at, sg.updated_at
	  FROM space_grants sg
	  LEFT JOIN orgs o   ON sg.principal_kind = 'org'   AND o.id = sg.principal_id
	  LEFT JOIN groups g ON sg.principal_kind = 'group' AND g.id = sg.principal_id
	  LEFT JOIN orgs go  ON go.id = g.org_id`

func scanSpaceGrant(sc interface{ Scan(...any) error }) (spaceGrantDTO, error) {
	var (
		g       spaceGrantDTO
		ctxName sql.NullString
	)
	err := sc.Scan(&g.ID, &g.PrincipalKind, &g.PrincipalID, &g.PrincipalName, &ctxName, &g.Role, &g.CreatedAt, &g.UpdatedAt)
	g.ContextName = nullableString(ctxName)
	return g, err
}

// ListSpaceGrants returns the org + group grants on a space. Any member reads.
func (s *Server) ListSpaceGrants(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := s.requireMembership(w, r, spaceID); !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(),
		spaceGrantSelect+` WHERE sg.space_id = $1 ORDER BY sg.principal_kind ASC, principal_name ASC`, spaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list space grants failed")
		return
	}
	defer rows.Close()

	grants := []spaceGrantDTO{}
	for rows.Next() {
		g, err := scanSpaceGrant(rows)
		if err != nil {
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

// AddSpaceGrant shares a space with an org or group at editor/viewer. Owner only.
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
	if !validPrincipalKind(req.PrincipalKind) {
		writeError(w, http.StatusBadRequest, "bad_request", "principal_kind must be one of org, group")
		return
	}
	if req.PrincipalID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "principal_id is required")
		return
	}
	if !grantRoleValid(req.Role) {
		writeError(w, http.StatusBadRequest, "bad_request", "role must be one of editor, viewer")
		return
	}

	ctx := r.Context()
	if exists, err := principalExists(ctx, s.DB, req.PrincipalKind, req.PrincipalID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup principal failed")
		return
	} else if !exists {
		writeError(w, http.StatusNotFound, "not_found", req.PrincipalKind+" not found")
		return
	}

	var id int64
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO space_grants (space_id, principal_kind, principal_id, role)
		VALUES ($1, $2, $3, $4) RETURNING id`, spaceID, req.PrincipalKind, req.PrincipalID, req.Role).Scan(&id)
	if err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "conflict", "this principal already has a grant on the space")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "add space grant failed")
		return
	}
	dto, err := selectSpaceGrant(ctx, s.DB, spaceID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch added grant failed")
		return
	}
	s.audit(ctx, r, "grant.add", "space", spaceID, dto.PrincipalKind+" "+dto.PrincipalName+" → "+req.Role)
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
	res, err := s.DB.ExecContext(ctx,
		`UPDATE space_grants SET role = $1, updated_at = tela_now() WHERE id = $2 AND space_id = $3`,
		req.Role, grantID, spaceID)
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
	s.audit(ctx, r, "grant.role", "space", spaceID, dto.PrincipalKind+" "+dto.PrincipalName+" → "+req.Role)
	writeJSON(w, http.StatusOK, map[string]any{"grant": dto})
}

// DeleteSpaceGrant revokes a principal's access to a space. Space owner only.
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
		`DELETE FROM space_grants WHERE id = $1 AND space_id = $2`, grantID, spaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete grant failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "grant not found")
		return
	}
	s.audit(r.Context(), r, "grant.remove", "space", spaceID, strconv.FormatInt(grantID, 10))
	w.WriteHeader(http.StatusNoContent)
}

func selectSpaceGrant(ctx context.Context, db *sql.DB, spaceID, grantID int64) (spaceGrantDTO, error) {
	row := db.QueryRowContext(ctx, spaceGrantSelect+` WHERE sg.id = $1 AND sg.space_id = $2`, grantID, spaceID)
	return scanSpaceGrant(row)
}
