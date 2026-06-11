package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

// First-run setup wizard (docs/operations.md). On a brand-new instance with no
// admin env configured, the env bootstrap (auth.BootstrapFromEnv) is a no-op and
// the users table is empty. These two public endpoints let the operator create
// the first admin through the web UI instead of the old env-var + logged-random-
// password dance:
//
//   GET  /api/setup/status → {"needs_setup": bool}  (true iff users table empty)
//   POST /api/setup        → create FIRST admin, provision space, sign in
//
// Both are on auth.IsPublicPath (no session yet). The write endpoint
// self-authenticates by the hardest gate there is: it only succeeds while the
// users table is empty, and that check is fused with the insert in one statement
// so two concurrent calls can never both create an admin.

// SetupStatus reports whether the instance still needs first-run setup, i.e. has
// no users at all. Public so the SPA can decide between /setup and /login before
// any account exists.
func (s *Server) SetupStatus(w http.ResponseWriter, r *http.Request) {
	empty, err := usersTableEmpty(r.Context(), s.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "setup status failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"needs_setup": empty})
}

type setupRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Setup creates the instance's FIRST admin (pre-verified email,
// is_instance_admin=1, is_active=1), provisions their personal space, and signs
// them in — the web equivalent of the env bootstrap. It is the only way to
// bootstrap an admin once TELA_ADMIN_PASSWORD is unset.
//
// Hard gate (security-sensitive): the INSERT carries its own
// `WHERE NOT EXISTS (SELECT 1 FROM users)` guard and RETURNs the new id. If the
// table is non-empty at the moment the row would be written, zero rows are
// inserted (sql.ErrNoRows on the RETURNING scan) and we answer 409 already_setup.
// Because the existence check and the insert are a single atomic statement, two
// concurrent setup calls race at the row level: exactly one inserts, the other
// sees a populated table and 409s — there is no check-then-insert window. The
// users.username/email unique indexes are a second backstop.
func (s *Server) Setup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	email := normalizeEmail(req.Email)
	username := strings.TrimSpace(req.Username)

	// Validate exactly like Register: a real email, a 1-64 char handle, an
	// 8+ char password.
	if !validEmail(email) {
		writeError(w, http.StatusBadRequest, "bad_request", "a valid email is required")
		return
	}
	if username == "" || len(username) > maxUsernameLen {
		writeError(w, http.StatusBadRequest, "bad_request", "username must be 1-64 characters")
		return
	}
	if len(req.Password) < minPasswordLen {
		writeError(w, http.StatusBadRequest, "bad_request", "password must be at least 8 characters")
		return
	}
	// Unified-handle guard: the username shares a public namespace with org
	// slugs, so reject reserved words / org-slug collisions (own-username
	// uniqueness is caught by the INSERT).
	if ae := checkHandleAvailable(r.Context(), username, orgSlugTaken, s.DB); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "hash password failed")
		return
	}

	ctx := r.Context()
	var userID int64
	// Create-if-empty must be serialized explicitly: a single
	// INSERT … WHERE NOT EXISTS is NOT atomic under READ COMMITTED — two
	// concurrent calls can each see an empty snapshot and both insert (with
	// distinct usernames there's no unique constraint to save us). An
	// advisory xact lock makes the second caller wait out the first commit,
	// after which its NOT EXISTS sees the row. The email is pre-verified
	// (the operator owns the box) so the admin can sign in immediately,
	// exactly like a confirmed registration.
	err = func() error {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('tela_first_run_setup'))`); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO users (username, email, email_verified_at, password_hash, is_instance_admin, is_active)
			SELECT $1, $2, tela_now(), $3, 1, 1
			 WHERE NOT EXISTS (SELECT 1 FROM users)
			RETURNING id`, username, email, hash).Scan(&userID); err != nil {
			return err
		}
		return tx.Commit()
	}()
	if errors.Is(err, sql.ErrNoRows) {
		// The table was non-empty → an admin already exists. Never create a
		// second one through this path.
		writeError(w, http.StatusConflict, "already_setup", "this instance has already been set up")
		return
	}
	if err != nil {
		if isUniqueConstraintErr(err) {
			// Username/email clash with an existing account.
			writeError(w, http.StatusConflict, "already_setup", "this instance has already been set up")
			return
		}
		slog.Error("setup: create first admin", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "create admin failed")
		return
	}

	// Provision the personal space and apply any auto-join domains, mirroring
	// the verified-registration sign-in path. Non-fatal: the account exists.
	if _, err := EnsurePersonalSpace(ctx, s.DB, userID, username); err != nil {
		slog.Error("setup: personal space", "user_id", userID, "err", err)
	}
	applyAutoJoin(ctx, s.DB, userID, email)

	dto, err := s.authUserByID(ctx, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "load user failed")
		return
	}
	sid, err := auth.CreateSession(ctx, s.DB, userID, r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create session failed")
		return
	}
	auth.SetSessionCookie(w, sid)
	s.recordRequestEvent(r, eventInput{Type: evtAuthLogin, ActorUserID: &userID, ActorLabel: username, Detail: "first-run setup"})
	slog.Info("first-run setup: instance admin created", "username", username)
	writeJSON(w, http.StatusOK, map[string]any{"user": dto})
}

// usersTableEmpty reports whether the instance has zero users.
func usersTableEmpty(ctx context.Context, d *sql.DB) (bool, error) {
	var exists bool
	err := d.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists)
	return !exists, err
}
