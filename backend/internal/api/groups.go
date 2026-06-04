package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/models"
)

const maxGroupNameLen = 200

// groupDTO is the wire shape for group listings.
type groupDTO struct {
	models.Group
	MemberCount int `json:"member_count"`
}

type groupCreateRequest struct {
	Name string `json:"name"`
}

type groupUpdateRequest struct {
	Name string `json:"name"`
}

// ListGroups returns an org's groups. Org member or instance-admin.
func (s *Server) ListGroups(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := s.requireOrgMember(w, r, orgID); !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT g.id, g.org_id, g.name, g.created_at, g.updated_at,
		       (SELECT COUNT(*) FROM group_members m WHERE m.group_id = g.id) AS member_count
		  FROM groups g
		 WHERE g.org_id = $1
		 ORDER BY g.name ASC`, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list groups failed")
		return
	}
	defer rows.Close()

	groups := []groupDTO{}
	for rows.Next() {
		var g groupDTO
		if err := rows.Scan(&g.ID, &g.OrgID, &g.Name, &g.CreatedAt, &g.UpdatedAt, &g.MemberCount); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan group row failed")
			return
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate groups failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

// myGroupDTO is the flat cross-org shape for the share picker: a group plus its
// org's name for grouping in the UI.
type myGroupDTO struct {
	ID      int64  `json:"id"`
	OrgID   int64  `json:"org_id"`
	Name    string `json:"name"`
	OrgName string `json:"org_name"`
}

// ListMyGroups returns every group the caller can grant a space to: groups in
// orgs they belong to (instance-admins see all). Powers the share-with-group
// picker without an N+1 over orgs.
func (s *Server) ListMyGroups(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	base := `
		SELECT g.id, g.org_id, g.name, o.name
		  FROM groups g
		  JOIN orgs o ON o.id = g.org_id`
	var (
		rows *sql.Rows
		err  error
	)
	if u.IsInstanceAdmin {
		rows, err = s.DB.QueryContext(r.Context(), base+` ORDER BY o.name ASC, g.name ASC`)
	} else {
		rows, err = s.DB.QueryContext(r.Context(),
			base+` WHERE g.org_id IN (SELECT org_id FROM org_members WHERE user_id = $1)
			 ORDER BY o.name ASC, g.name ASC`, u.ID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list groups failed")
		return
	}
	defer rows.Close()

	groups := []myGroupDTO{}
	for rows.Next() {
		var g myGroupDTO
		if err := rows.Scan(&g.ID, &g.OrgID, &g.Name, &g.OrgName); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan group row failed")
			return
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate groups failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

// CreateGroup adds a group to an org. Org admin or instance-admin.
func (s *Server) CreateGroup(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	var req groupCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > maxGroupNameLen {
		writeError(w, http.StatusBadRequest, "invalid_name", "name must be 1-200 characters")
		return
	}

	var id int64
	err := s.DB.QueryRowContext(r.Context(),
		`INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id`, orgID, name).Scan(&id)
	if err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "conflict", "a group with that name already exists in this org")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "create group failed")
		return
	}
	g, err := selectGroupByID(r.Context(), s.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch created group failed")
		return
	}
	s.audit(r.Context(), r, "group.create", "org", orgID, g.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"group": groupDTO{Group: g}})
}

// UpdateGroup renames a group. Org admin or instance-admin.
func (s *Server) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	groupID, ok := parseIDParam(w, r, "group_id")
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	if _, ok := s.requireGroupInOrg(w, r, orgID, groupID); !ok {
		return
	}
	var req groupUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > maxGroupNameLen {
		writeError(w, http.StatusBadRequest, "invalid_name", "name must be 1-200 characters")
		return
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE groups SET name = $1, updated_at = tela_now() WHERE id = $2`, name, groupID); err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "conflict", "a group with that name already exists in this org")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "update group failed")
		return
	}
	g, err := selectGroupByID(r.Context(), s.DB, groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch updated group failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": groupDTO{Group: g}})
}

// DeleteGroup removes a group. Org admin or instance-admin. group_members
// cascade via FK; the group's space_grants have no FK (polymorphic principal)
// and are deleted explicitly in the same tx.
func (s *Server) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	groupID, ok := parseIDParam(w, r, "group_id")
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	if _, ok := s.requireGroupInOrg(w, r, orgID, groupID); !ok {
		return
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM space_grants WHERE principal_kind = 'group' AND principal_id = $1`, groupID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "remove group grants failed")
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM groups WHERE id = $1`, groupID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete group failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	s.audit(ctx, r, "group.delete", "org", orgID, "")
	w.WriteHeader(http.StatusNoContent)
}

// requireGroupInOrg loads the group and verifies it belongs to orgID, writing
// 404 otherwise. Returns the group on success.
func (s *Server) requireGroupInOrg(w http.ResponseWriter, r *http.Request, orgID, groupID int64) (models.Group, bool) {
	g, err := selectGroupByID(r.Context(), s.DB, groupID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && g.OrgID != orgID) {
		writeError(w, http.StatusNotFound, "not_found", "group not found")
		return models.Group{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup group failed")
		return models.Group{}, false
	}
	return g, true
}

func selectGroupByID(ctx context.Context, db *sql.DB, id int64) (models.Group, error) {
	var g models.Group
	err := db.QueryRowContext(ctx,
		`SELECT id, org_id, name, created_at, updated_at FROM groups WHERE id = $1`, id,
	).Scan(&g.ID, &g.OrgID, &g.Name, &g.CreatedAt, &g.UpdatedAt)
	return g, err
}
