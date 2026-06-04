package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// groupMemberDTO is the wire shape for group member listings.
type groupMemberDTO struct {
	UserID    int64   `json:"user_id"`
	Username  string  `json:"username"`
	Email     *string `json:"email"`
	CreatedAt string  `json:"created_at"`
}

type groupMemberAddRequest struct {
	// Identifier is the target user's email or username. They must already be a
	// member of the group's org (group membership ⊆ org membership).
	Identifier string `json:"identifier"`
}

// ListGroupMembers returns a group's members. Org member or instance-admin.
func (s *Server) ListGroupMembers(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	groupID, ok := parseIDParam(w, r, "group_id")
	if !ok {
		return
	}
	if _, ok := s.requireOrgMember(w, r, orgID); !ok {
		return
	}
	if _, ok := s.requireGroupInOrg(w, r, orgID, groupID); !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT gm.user_id, u.username, u.email, gm.created_at
		  FROM group_members gm
		  JOIN users u ON u.id = gm.user_id
		 WHERE gm.group_id = ?
		 ORDER BY u.username ASC`, groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list group members failed")
		return
	}
	defer rows.Close()

	members := []groupMemberDTO{}
	for rows.Next() {
		var (
			m     groupMemberDTO
			email sql.NullString
		)
		if err := rows.Scan(&m.UserID, &m.Username, &email, &m.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan group member row failed")
			return
		}
		m.Email = nullableString(email)
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate group members failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members})
}

// AddGroupMember adds an existing org member to a group. Org admin or
// instance-admin. The target must already belong to the org (enforced here for
// a clean error, and by a DB trigger as backstop).
func (s *Server) AddGroupMember(w http.ResponseWriter, r *http.Request) {
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
	var req groupMemberAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	identifier := strings.TrimSpace(req.Identifier)
	if identifier == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "identifier (email or username) is required")
		return
	}

	ctx := r.Context()
	var targetID int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM users WHERE (username = ? OR email = ?) AND is_active = 1`,
		identifier, normalizeEmail(identifier)).Scan(&targetID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup user failed")
		return
	}

	// Group membership ⊆ org membership — clean error before the DB trigger fires.
	if _, err := orgRole(ctx, s.DB, targetID, orgID); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusConflict, "not_org_member", "add them to the org before adding them to a group")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "check org membership failed")
		return
	}

	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES (?, ?)`, groupID, targetID); err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "conflict", "user is already in this group")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "add group member failed")
		return
	}
	dto, err := selectGroupMember(ctx, s.DB, groupID, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch added member failed")
		return
	}
	s.audit(ctx, r, "group_member.add", "org", orgID, dto.Username)
	writeJSON(w, http.StatusCreated, map[string]any{"member": dto})
}

// DeleteGroupMember removes a member from a group. Org admin / instance-admin,
// or the member themselves (self-leave). No last-anything guard — groups have
// no required role.
func (s *Server) DeleteGroupMember(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	groupID, ok := parseIDParam(w, r, "group_id")
	if !ok {
		return
	}
	targetID, ok := parseIDParam(w, r, "user_id")
	if !ok {
		return
	}
	caller, ok := requireUser(w, r)
	if !ok {
		return
	}
	role, ok := s.requireOrgMember(w, r, orgID)
	if !ok {
		return
	}
	if role != orgRoleAdmin && caller.ID != targetID {
		writeError(w, http.StatusForbidden, "forbidden", "org admin required")
		return
	}
	if _, ok := s.requireGroupInOrg(w, r, orgID, groupID); !ok {
		return
	}

	res, err := s.DB.ExecContext(r.Context(),
		`DELETE FROM group_members WHERE group_id = ? AND user_id = ?`, groupID, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete group member failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "member not found")
		return
	}
	s.audit(r.Context(), r, "group_member.remove", "org", orgID, strconv.FormatInt(targetID, 10))
	w.WriteHeader(http.StatusNoContent)
}

func selectGroupMember(ctx context.Context, d *sql.DB, groupID, userID int64) (groupMemberDTO, error) {
	var (
		m     groupMemberDTO
		email sql.NullString
	)
	err := d.QueryRowContext(ctx, `
		SELECT gm.user_id, u.username, u.email, gm.created_at
		  FROM group_members gm JOIN users u ON u.id = gm.user_id
		 WHERE gm.group_id = ? AND gm.user_id = ?`, groupID, userID).
		Scan(&m.UserID, &m.Username, &email, &m.CreatedAt)
	m.Email = nullableString(email)
	return m, err
}
