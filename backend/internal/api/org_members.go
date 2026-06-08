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

// orgMemberDTO is the wire shape for org member listings + writes.
type orgMemberDTO struct {
	UserID    int64   `json:"user_id"`
	Username  string  `json:"username"`
	Email     *string `json:"email"`
	OrgRole   string  `json:"org_role"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

type orgMemberAddRequest struct {
	// Identifier is the target user's email or username.
	Identifier string `json:"identifier"`
	OrgRole    string `json:"org_role"`
}

type orgMemberPatchRequest struct {
	OrgRole string `json:"org_role"`
}

// ListOrgMembers returns every membership row for an org. Member or
// instance-admin. Ordered admin→member then username.
func (s *Server) ListOrgMembers(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := s.requireOrgMember(w, r, orgID); !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT om.user_id, u.username, u.email, om.org_role, om.created_at, om.updated_at
		  FROM org_members om
		  JOIN users u ON u.id = om.user_id
		 WHERE om.org_id = $1
		 ORDER BY om.org_role ASC, u.username ASC`, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list org members failed")
		return
	}
	defer rows.Close()

	members := []orgMemberDTO{}
	for rows.Next() {
		m, err := scanOrgMemberRow(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan org member row failed")
			return
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate org members failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members})
}

// AddOrgMember adds an existing active user (by email or username) to an org.
// Org admin or instance-admin. 404 unknown user, 409 already a member.
func (s *Server) AddOrgMember(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	var req orgMemberAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	identifier := strings.TrimSpace(req.Identifier)
	if identifier == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "identifier (email or username) is required")
		return
	}
	if !isValidOrgRole(req.OrgRole) {
		writeError(w, http.StatusBadRequest, "bad_request", "org_role must be one of admin, member")
		return
	}

	ctx := r.Context()
	var targetID int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM users WHERE (username = $1 OR email = $2) AND is_active = 1`,
		identifier, normalizeEmail(identifier)).Scan(&targetID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup user failed")
		return
	}

	if ae := s.checkSeatQuota(ctx, orgID); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}

	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO org_members (org_id, user_id, org_role) VALUES ($1, $2, $3)`,
		orgID, targetID, req.OrgRole); err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "conflict", "user is already a member")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "add org member failed")
		return
	}
	dto, err := selectOrgMember(ctx, s.DB, orgID, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch added member failed")
		return
	}
	s.audit(ctx, r, "org_member.add", "org", orgID, req.OrgRole+": "+dto.Username)
	writeJSON(w, http.StatusCreated, map[string]any{"member": dto})
}

// PatchOrgMember changes a member's org_role. Org admin or instance-admin.
// Refuses to demote the last admin.
func (s *Server) PatchOrgMember(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	userID, ok := parseIDParam(w, r, "user_id")
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	var req orgMemberPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if !isValidOrgRole(req.OrgRole) {
		writeError(w, http.StatusBadRequest, "bad_request", "org_role must be one of admin, member")
		return
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	var existingRole string
	err = tx.QueryRowContext(ctx,
		`SELECT org_role FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID).Scan(&existingRole)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "member not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup member failed")
		return
	}

	if existingRole == orgRoleAdmin && req.OrgRole != orgRoleAdmin {
		if last, err := wouldLeaveZeroOrgAdminsTx(ctx, tx, orgID, userID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "count org admins failed")
			return
		} else if last {
			writeError(w, http.StatusBadRequest, "last_admin", "cannot demote the last admin of the org")
			return
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE org_members SET org_role = $1, updated_at = tela_now()
		 WHERE org_id = $2 AND user_id = $3`, req.OrgRole, orgID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "update member failed")
		return
	}
	dto, err := selectOrgMemberTx(ctx, tx, orgID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch updated member failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	s.audit(ctx, r, "org_member.role", "org", orgID, dto.Username+" → "+req.OrgRole)
	writeJSON(w, http.StatusOK, map[string]any{"member": dto})
}

// DeleteOrgMember removes a member. Org admin / instance-admin, or the member
// themselves (self-leave). Last-admin safeguard applies to admin rows.
func (s *Server) DeleteOrgMember(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseIDParam(w, r, "id")
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
	// Self-leave is allowed for any member; otherwise require admin (org or
	// instance). requireOrgMember resolves the caller's role + the org's
	// existence in one shot.
	callerRole, ok := s.requireOrgMember(w, r, orgID)
	if !ok {
		return
	}
	if callerRole != orgRoleAdmin && caller.ID != targetID {
		writeError(w, http.StatusForbidden, "forbidden", "org admin required")
		return
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	var existingRole string
	err = tx.QueryRowContext(ctx,
		`SELECT org_role FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, targetID).Scan(&existingRole)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "member not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup member failed")
		return
	}

	if existingRole == orgRoleAdmin {
		if last, err := wouldLeaveZeroOrgAdminsTx(ctx, tx, orgID, targetID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "count org admins failed")
			return
		} else if last {
			writeError(w, http.StatusBadRequest, "last_admin", "cannot remove the last admin of the org")
			return
		}
	}

	// Identity-derived membership is non-discretionary: a member whose verified
	// email domain maps to this org can't be removed (login would re-add them).
	// Remove the domain mapping instead. See docs/access-model.md.
	if isDomainManagedMember(ctx, tx, orgID, targetID) {
		writeError(w, http.StatusConflict, "domain_managed",
			"this member is auto-joined via an email-domain mapping — remove the mapping to remove them")
		return
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete member failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	s.audit(ctx, r, "org_member.remove", "org", orgID, strconv.FormatInt(targetID, 10))
	w.WriteHeader(http.StatusNoContent)
}

// wouldLeaveZeroOrgAdminsTx reports whether removing/demoting excludeUserID
// would leave the org with no admins.
func wouldLeaveZeroOrgAdminsTx(ctx context.Context, tx *sql.Tx, orgID, excludeUserID int64) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM org_members
		 WHERE org_id = $1 AND org_role = 'admin' AND user_id != $2`, orgID, excludeUserID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

type orgMemberScanner interface {
	Scan(dest ...any) error
}

func scanOrgMemberRow(s orgMemberScanner) (orgMemberDTO, error) {
	var (
		m     orgMemberDTO
		email sql.NullString
	)
	if err := s.Scan(&m.UserID, &m.Username, &email, &m.OrgRole, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return orgMemberDTO{}, err
	}
	m.Email = nullableString(email)
	return m, nil
}

func selectOrgMember(ctx context.Context, d *sql.DB, orgID, userID int64) (orgMemberDTO, error) {
	row := d.QueryRowContext(ctx, `
		SELECT om.user_id, u.username, u.email, om.org_role, om.created_at, om.updated_at
		  FROM org_members om JOIN users u ON u.id = om.user_id
		 WHERE om.org_id = $1 AND om.user_id = $2`, orgID, userID)
	return scanOrgMemberRow(row)
}

func selectOrgMemberTx(ctx context.Context, tx *sql.Tx, orgID, userID int64) (orgMemberDTO, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT om.user_id, u.username, u.email, om.org_role, om.created_at, om.updated_at
		  FROM org_members om JOIN users u ON u.id = om.user_id
		 WHERE om.org_id = $1 AND om.user_id = $2`, orgID, userID)
	return scanOrgMemberRow(row)
}
