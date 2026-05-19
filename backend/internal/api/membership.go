package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/zcag/tela/backend/internal/auth"
)

const (
	roleOwner  = "owner"
	roleEditor = "editor"
	roleViewer = "viewer"
)

// canEdit is true for owner + editor — the gate for any page mutation
// (CREATE/PATCH/DELETE/move).
func canEdit(role string) bool {
	return role == roleOwner || role == roleEditor
}

// requireUser pulls the authenticated user off the request context. Returns
// (nil, false) and writes 401 when called outside the middleware — shouldn't
// happen for wrapped routes, but defends against accidental misuse.
func requireUser(w http.ResponseWriter, r *http.Request) (*auth.User, bool) {
	u, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return nil, false
	}
	return u, true
}

// requireInstanceAdmin gates an endpoint to instance-admins only. Writes the
// 401 envelope when the caller isn't authenticated (defensive — should be
// caught by middleware), or the 403 envelope when the caller is authenticated
// but not an instance admin.
func requireInstanceAdmin(w http.ResponseWriter, r *http.Request) (*auth.User, bool) {
	u, ok := requireUser(w, r)
	if !ok {
		return nil, false
	}
	if !u.IsInstanceAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "instance admin required")
		return nil, false
	}
	return u, true
}

// spaceRole returns the user's role for spaceID, or sql.ErrNoRows when they
// are not a member.
func spaceRole(ctx context.Context, db *sql.DB, userID, spaceID int64) (string, error) {
	var role string
	err := db.QueryRowContext(ctx,
		`SELECT role FROM space_members WHERE space_id = ? AND user_id = ?`,
		spaceID, userID).Scan(&role)
	return role, err
}

// spaceRoleTx is the in-tx variant of spaceRole, used by handlers that need
// the membership check inside an existing transaction.
func spaceRoleTx(ctx context.Context, tx *sql.Tx, userID, spaceID int64) (string, error) {
	var role string
	err := tx.QueryRowContext(ctx,
		`SELECT role FROM space_members WHERE space_id = ? AND user_id = ?`,
		spaceID, userID).Scan(&role)
	return role, err
}

// requireMembership resolves the user's role in spaceID and writes the
// appropriate 401/403/500 envelope when access should be denied. Returns
// (role, true) on success; (_, false) means the response has been written and
// the caller must return immediately.
func (s *Server) requireMembership(w http.ResponseWriter, r *http.Request, spaceID int64) (string, bool) {
	u, ok := requireUser(w, r)
	if !ok {
		return "", false
	}
	role, err := spaceRole(r.Context(), s.DB, u.ID, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return "", false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup membership failed")
		return "", false
	}
	return role, true
}
