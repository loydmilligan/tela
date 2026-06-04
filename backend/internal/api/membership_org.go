package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/zcag/tela/backend/internal/auth"
)

const (
	orgRoleAdmin  = "admin"
	orgRoleMember = "member"
)

func isValidOrgRole(r string) bool {
	return r == orgRoleAdmin || r == orgRoleMember
}

// orgRole returns the user's org_role for orgID, or sql.ErrNoRows when they are
// not a member.
func orgRole(ctx context.Context, db *sql.DB, userID, orgID int64) (string, error) {
	var role string
	err := db.QueryRowContext(ctx,
		`SELECT org_role FROM org_members WHERE org_id = ? AND user_id = ?`,
		orgID, userID).Scan(&role)
	return role, err
}

// orgExists reports whether orgID is a real org. Used to return 404 (vs 403)
// when an instance-admin targets a missing org.
func orgExists(ctx context.Context, db *sql.DB, orgID int64) (bool, error) {
	var x int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM orgs WHERE id = ?`, orgID).Scan(&x)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// requireOrgMember resolves the caller's relationship to orgID. Instance-admins
// pass as a virtual admin (so they can administer any tenant) even without a
// membership row. Returns (org_role, true) on success; writes 403/404 and
// returns false otherwise. For instance-admins who aren't members the returned
// role is orgRoleAdmin.
func (s *Server) requireOrgMember(w http.ResponseWriter, r *http.Request, orgID int64) (string, bool) {
	u, ok := requireUser(w, r)
	if !ok {
		return "", false
	}
	// Instance-admins are virtual admins of EVERY org — always, regardless of
	// whether they also hold a (possibly lower) membership row. Resolve this
	// first so an instance-admin who joined an org as a plain member doesn't
	// lose their superuser authority over it.
	if u.IsInstanceAdmin {
		if exists, err := orgExists(r.Context(), s.DB, orgID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "lookup org failed")
			return "", false
		} else if !exists {
			writeError(w, http.StatusNotFound, "not_found", "org not found")
			return "", false
		}
		return orgRoleAdmin, true
	}
	role, err := orgRole(r.Context(), s.DB, u.ID, orgID)
	if err == nil {
		return role, true
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member of this org")
		return "", false
	}
	writeError(w, http.StatusInternalServerError, "internal", "lookup org membership failed")
	return "", false
}

// requireOrgAdmin is requireOrgMember plus the admin gate. Instance-admins
// always pass.
func (s *Server) requireOrgAdmin(w http.ResponseWriter, r *http.Request, orgID int64) bool {
	role, ok := s.requireOrgMember(w, r, orgID)
	if !ok {
		return false
	}
	if role != orgRoleAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "org admin required")
		return false
	}
	return true
}

// callerIsInstanceAdmin is a non-writing check used where instance-admin
// unlocks an extra capability (e.g. seeing all orgs) but absence isn't an
// error.
func callerIsInstanceAdmin(r *http.Request) bool {
	u, ok := auth.UserFromContext(r.Context())
	return ok && u.IsInstanceAdmin
}
