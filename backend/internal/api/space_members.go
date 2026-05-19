package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// spaceMemberDTO is the wire shape for member listings + writes. Joins
// users to expose username alongside the membership row.
type spaceMemberDTO struct {
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type spaceMemberAddRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

type spaceMemberPatchRequest struct {
	Role string `json:"role"`
}

// ListSpaceMembers returns every membership row for a space. Any member can
// read; ordered owner→editor→viewer (role ASC sorts that way alphabetically)
// then by username ASC.
func (s *Server) ListSpaceMembers(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := s.requireMembership(w, r, spaceID); !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT sm.user_id, u.username, sm.role, sm.created_at, sm.updated_at
		  FROM space_members sm
		  JOIN users u ON u.id = sm.user_id
		 WHERE sm.space_id = ?
		 ORDER BY sm.role ASC, u.username ASC`, spaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list members failed")
		return
	}
	defer rows.Close()

	members := []spaceMemberDTO{}
	for rows.Next() {
		var m spaceMemberDTO
		if err := rows.Scan(&m.UserID, &m.Username, &m.Role, &m.CreatedAt, &m.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan member row failed")
			return
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate members failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members})
}

// AddSpaceMember adds an existing user to a space with the given role. Owner
// only. 404 when the user doesn't exist or is inactive; 409 when already a
// member.
func (s *Server) AddSpaceMember(w http.ResponseWriter, r *http.Request) {
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

	var req spaceMemberAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "username is required")
		return
	}
	if !isValidRole(req.Role) {
		writeError(w, http.StatusBadRequest, "bad_request", "role must be one of owner, editor, viewer")
		return
	}

	ctx := r.Context()
	var (
		targetID int64
	)
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM users WHERE username = ? AND is_active = 1`, username).Scan(&targetID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup user failed")
		return
	}

	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO space_members (space_id, user_id, role) VALUES (?, ?, ?)`,
		spaceID, targetID, req.Role); err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "conflict", "user is already a member")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "add member failed")
		return
	}

	dto, err := selectSpaceMember(ctx, s.DB, spaceID, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch added member failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"member": dto})
}

// PatchSpaceMember changes a member's role. Owner only. Refuses to demote
// the last owner of the space.
func (s *Server) PatchSpaceMember(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	userID, ok := parseIDParam(w, r, "user_id")
	if !ok {
		return
	}
	callerRole, ok := s.requireMembership(w, r, spaceID)
	if !ok {
		return
	}
	if callerRole != roleOwner {
		writeError(w, http.StatusForbidden, "forbidden", "owner role required")
		return
	}

	var req spaceMemberPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if !isValidRole(req.Role) {
		writeError(w, http.StatusBadRequest, "bad_request", "role must be one of owner, editor, viewer")
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
		`SELECT role FROM space_members WHERE space_id = ? AND user_id = ?`, spaceID, userID).
		Scan(&existingRole)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "member not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup member failed")
		return
	}

	if existingRole == roleOwner && req.Role != roleOwner {
		if last, err := wouldLeaveZeroOwnersTx(ctx, tx, spaceID, userID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "count owners failed")
			return
		} else if last {
			writeError(w, http.StatusBadRequest, "last_owner", "cannot demote the last owner of the space")
			return
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE space_members
		   SET role = ?, updated_at = datetime('now')
		 WHERE space_id = ? AND user_id = ?`, req.Role, spaceID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "update member failed")
		return
	}
	dto, err := selectSpaceMemberTx(ctx, tx, spaceID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch updated member failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"member": dto})
}

// DeleteSpaceMember removes a member from a space. Owners or the member
// themselves (self-leave for non-owners). Last-owner safeguard applies when
// the row being removed is an owner.
func (s *Server) DeleteSpaceMember(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
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
	callerRole, ok := s.requireMembership(w, r, spaceID)
	if !ok {
		return
	}
	// Self-leave allowed for any member; otherwise owner only.
	if callerRole != roleOwner && caller.ID != targetID {
		writeError(w, http.StatusForbidden, "forbidden", "owner role required")
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
		`SELECT role FROM space_members WHERE space_id = ? AND user_id = ?`, spaceID, targetID).
		Scan(&existingRole)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "member not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup member failed")
		return
	}

	if existingRole == roleOwner {
		if last, err := wouldLeaveZeroOwnersTx(ctx, tx, spaceID, targetID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "count owners failed")
			return
		} else if last {
			writeError(w, http.StatusBadRequest, "last_owner", "cannot remove the last owner of the space")
			return
		}
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM space_members WHERE space_id = ? AND user_id = ?`, spaceID, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete member failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func isValidRole(r string) bool {
	return r == roleOwner || r == roleEditor || r == roleViewer
}

// wouldLeaveZeroOwnersTx returns true if removing or demoting excludeUserID
// from spaceID would leave the space with zero owners. Run inside the same
// tx as the mutation to dodge concurrent-demote races.
func wouldLeaveZeroOwnersTx(ctx context.Context, tx *sql.Tx, spaceID, excludeUserID int64) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM space_members
		 WHERE space_id = ? AND role = 'owner' AND user_id != ?`, spaceID, excludeUserID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

func selectSpaceMember(ctx context.Context, d *sql.DB, spaceID, userID int64) (spaceMemberDTO, error) {
	var dto spaceMemberDTO
	err := d.QueryRowContext(ctx, `
		SELECT sm.user_id, u.username, sm.role, sm.created_at, sm.updated_at
		  FROM space_members sm
		  JOIN users u ON u.id = sm.user_id
		 WHERE sm.space_id = ? AND sm.user_id = ?`, spaceID, userID).
		Scan(&dto.UserID, &dto.Username, &dto.Role, &dto.CreatedAt, &dto.UpdatedAt)
	return dto, err
}

func selectSpaceMemberTx(ctx context.Context, tx *sql.Tx, spaceID, userID int64) (spaceMemberDTO, error) {
	var dto spaceMemberDTO
	err := tx.QueryRowContext(ctx, `
		SELECT sm.user_id, u.username, sm.role, sm.created_at, sm.updated_at
		  FROM space_members sm
		  JOIN users u ON u.id = sm.user_id
		 WHERE sm.space_id = ? AND sm.user_id = ?`, spaceID, userID).
		Scan(&dto.UserID, &dto.Username, &dto.Role, &dto.CreatedAt, &dto.UpdatedAt)
	return dto, err
}
