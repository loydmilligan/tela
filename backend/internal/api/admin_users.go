package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

const (
	minPasswordLen = 8
	maxUsernameLen = 64
)

// adminUserDTO is the wire shape for admin user listings + writes. Mirrors
// the users row except password_hash (never exposed).
type adminUserDTO struct {
	ID              int64   `json:"id"`
	Username        string  `json:"username"`
	Email           *string `json:"email"`
	EmailVerified   bool    `json:"email_verified"`
	IsInstanceAdmin bool    `json:"is_instance_admin"`
	IsActive        bool    `json:"is_active"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type adminUserCreateRequest struct {
	Username        string `json:"username"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	IsInstanceAdmin *bool  `json:"is_instance_admin"`
}

type adminUserPatchRequest struct {
	IsActive        *bool   `json:"is_active"`
	IsInstanceAdmin *bool   `json:"is_instance_admin"`
	Password        *string `json:"password"`
}

// ListAdminUsers returns every user row, including inactive ones, sorted by
// username ASC. Instance-admin only.
func (s *Server) ListAdminUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT id, username, email, email_verified_at, is_instance_admin, is_active, created_at, updated_at
		  FROM users
		 ORDER BY username ASC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list users failed")
		return
	}
	defer rows.Close()

	users := []adminUserDTO{}
	for rows.Next() {
		u, err := scanAdminUserRow(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan user row failed")
			return
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate users failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// CreateAdminUser inserts a new user. 409 on duplicate username, 400 on
// validation failure, 201 with the new row otherwise. Instance-admin only.
func (s *Server) CreateAdminUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}

	var req adminUserCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || len(username) > maxUsernameLen {
		writeError(w, http.StatusBadRequest, "bad_request", "username must be 1-64 characters")
		return
	}
	if len(req.Password) < minPasswordLen {
		writeError(w, http.StatusBadRequest, "bad_request", "password must be at least 8 characters")
		return
	}
	// Email is optional for admin-created users. When present it must be valid;
	// admin-created accounts are treated as pre-confirmed (no verify email).
	var email, verifiedAt any
	if raw := normalizeEmail(req.Email); raw != "" {
		if !validEmail(raw) {
			writeError(w, http.StatusBadRequest, "bad_request", "email must be a valid address")
			return
		}
		email = raw
		verifiedAt = nowStamp()
	}
	isAdmin := 0
	if req.IsInstanceAdmin != nil && *req.IsInstanceAdmin {
		isAdmin = 1
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "hash password failed")
		return
	}

	ctx := r.Context()
	var id int64
	err = s.DB.QueryRowContext(ctx, `
		INSERT INTO users (username, email, email_verified_at, password_hash, is_instance_admin, is_active)
		VALUES ($1, $2, $3, $4, $5, 1) RETURNING id`, username, email, verifiedAt, hash, isAdmin).Scan(&id)
	if err != nil {
		if isUniqueConstraintErr(err) {
			writeError(w, http.StatusConflict, "conflict", "username or email already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "create user failed")
		return
	}
	// Provision the new user's personal space (best-effort: the user already
	// exists, so a provisioning hiccup shouldn't fail the create — the startup
	// backfill will catch it on next boot).
	if _, err := EnsurePersonalSpace(ctx, s.DB, id, username); err != nil {
		log.Printf("personal space for new user %d (%s): %v", id, username, err)
	}
	dto, err := selectAdminUserByID(ctx, s.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch created user failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": dto})
}

// PatchAdminUser updates is_active, is_instance_admin, and/or password on a
// target user. Safeguards: caller can't self-target, can't demote/deactivate
// the last instance-admin. Password reset + is_active=false both clear all
// sessions for the target user in the same tx.
func (s *Server) PatchAdminUser(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireInstanceAdmin(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}

	var req adminUserPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if req.IsActive == nil && req.IsInstanceAdmin == nil && req.Password == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "at least one of is_active, is_instance_admin, password must be provided")
		return
	}
	if id == caller.ID {
		writeError(w, http.StatusBadRequest, "bad_request", "cannot modify self via admin endpoint")
		return
	}

	var newHash string
	if req.Password != nil {
		if len(*req.Password) < minPasswordLen {
			writeError(w, http.StatusBadRequest, "bad_request", "password must be at least 8 characters")
			return
		}
		h, err := auth.HashPassword(*req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "hash password failed")
			return
		}
		newHash = h
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	var (
		existingActive  int
		existingIsAdmin int
	)
	err = tx.QueryRowContext(ctx,
		`SELECT is_active, is_instance_admin FROM users WHERE id = $1`, id).
		Scan(&existingActive, &existingIsAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup user failed")
		return
	}

	demotingAdmin := existingIsAdmin == 1 && req.IsInstanceAdmin != nil && !*req.IsInstanceAdmin
	deactivatingAdmin := existingIsAdmin == 1 && existingActive == 1 && req.IsActive != nil && !*req.IsActive
	if demotingAdmin || deactivatingAdmin {
		if last, err := wouldLeaveZeroAdminsTx(ctx, tx, id); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "count admins failed")
			return
		} else if last {
			writeError(w, http.StatusBadRequest, "last_admin", "cannot demote or deactivate the last instance admin")
			return
		}
	}

	sets := make([]string, 0, 4)
	args := make([]any, 0, 4)
	if req.IsActive != nil {
		v := 0
		if *req.IsActive {
			v = 1
		}
		sets = append(sets, "is_active = $"+strconv.Itoa(len(args)+1))
		args = append(args, v)
	}
	if req.IsInstanceAdmin != nil {
		v := 0
		if *req.IsInstanceAdmin {
			v = 1
		}
		sets = append(sets, "is_instance_admin = $"+strconv.Itoa(len(args)+1))
		args = append(args, v)
	}
	if req.Password != nil {
		sets = append(sets, "password_hash = $"+strconv.Itoa(len(args)+1))
		args = append(args, newHash)
	}
	sets = append(sets, "updated_at = tela_now()")
	stmt := "UPDATE users SET " + strings.Join(sets, ", ") + " WHERE id = $" + strconv.Itoa(len(args)+1)
	args = append(args, id)
	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "update user failed")
		return
	}

	// Kill all sessions if password reset or deactivation took effect.
	wipeSessions := req.Password != nil ||
		(req.IsActive != nil && !*req.IsActive && existingActive == 1)
	if wipeSessions {
		if err := auth.DeleteUserSessions(ctx, tx, id); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "clear sessions failed")
			return
		}
	}

	dto, err := selectAdminUserByIDTx(ctx, tx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch updated user failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": dto})
}

// DeleteAdminUser soft-deletes a user (is_active=0) and wipes their sessions.
// Idempotent on already-inactive users. Same safeguards as PATCH:
// no self-target, can't deactivate the last instance admin.
func (s *Server) DeleteAdminUser(w http.ResponseWriter, r *http.Request) {
	caller, ok := requireInstanceAdmin(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if id == caller.ID {
		writeError(w, http.StatusBadRequest, "bad_request", "cannot modify self via admin endpoint")
		return
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	var (
		existingActive  int
		existingIsAdmin int
	)
	err = tx.QueryRowContext(ctx,
		`SELECT is_active, is_instance_admin FROM users WHERE id = $1`, id).
		Scan(&existingActive, &existingIsAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup user failed")
		return
	}

	if existingIsAdmin == 1 && existingActive == 1 {
		if last, err := wouldLeaveZeroAdminsTx(ctx, tx, id); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "count admins failed")
			return
		} else if last {
			writeError(w, http.StatusBadRequest, "last_admin", "cannot deactivate the last instance admin")
			return
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET is_active = 0, updated_at = tela_now() WHERE id = $1`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "deactivate user failed")
		return
	}
	if err := auth.DeleteUserSessions(ctx, tx, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "clear sessions failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// wouldLeaveZeroAdminsTx returns true if removing or demoting excludeID from
// the active instance-admin set would drop the count to zero. Counted inside
// the same tx as the mutation so a concurrent demote can't race.
func wouldLeaveZeroAdminsTx(ctx context.Context, tx *sql.Tx, excludeID int64) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM users
		 WHERE is_active = 1 AND is_instance_admin = 1 AND id != $1`, excludeID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

type adminUserScanner interface {
	Scan(dest ...any) error
}

func scanAdminUserRow(s adminUserScanner) (adminUserDTO, error) {
	var (
		dto             adminUserDTO
		email, verified sql.NullString
		isAdmin, active int
	)
	if err := s.Scan(&dto.ID, &dto.Username, &email, &verified, &isAdmin, &active, &dto.CreatedAt, &dto.UpdatedAt); err != nil {
		return adminUserDTO{}, err
	}
	dto.Email = nullableString(email)
	dto.EmailVerified = verified.Valid
	dto.IsInstanceAdmin = isAdmin == 1
	dto.IsActive = active == 1
	return dto, nil
}

func selectAdminUserByID(ctx context.Context, d *sql.DB, id int64) (adminUserDTO, error) {
	row := d.QueryRowContext(ctx, `
		SELECT id, username, email, email_verified_at, is_instance_admin, is_active, created_at, updated_at
		  FROM users WHERE id = $1`, id)
	return scanAdminUserRow(row)
}

func selectAdminUserByIDTx(ctx context.Context, tx *sql.Tx, id int64) (adminUserDTO, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, username, email, email_verified_at, is_instance_admin, is_active, created_at, updated_at
		  FROM users WHERE id = $1`, id)
	return scanAdminUserRow(row)
}
