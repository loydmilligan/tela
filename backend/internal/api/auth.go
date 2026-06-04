package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

type authLoginRequest struct {
	// Identifier is an email or a username. The legacy Username field is still
	// accepted as a fallback so older clients keep working.
	Identifier string `json:"identifier"`
	Username   string `json:"username"`
	Password   string `json:"password"`
}

type authUserDTO struct {
	ID              int64   `json:"id"`
	Username        string  `json:"username"`
	Email           *string `json:"email"`
	EmailVerified   bool    `json:"email_verified"`
	IsInstanceAdmin bool    `json:"is_instance_admin"`
}

// Login authenticates an email-or-username + password pair. On success it
// creates a session, sets the canonical cookie, and returns the user. Failure
// returns a single generic 401 envelope (no bad-user vs bad-password split; the
// argon2id verify still runs against a dummy hash when the user is missing to
// keep response time roughly constant). An account that has an email but hasn't
// confirmed it gets a 403 email_unverified so the UI can offer to resend.
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var req authLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	identifier := strings.TrimSpace(req.Identifier)
	if identifier == "" {
		identifier = strings.TrimSpace(req.Username)
	}
	if identifier == "" || req.Password == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}

	ctx := r.Context()
	var (
		userID        int64
		username      string
		email         sql.NullString
		emailVerified sql.NullString
		hash          string
		isAdmin       int
	)
	// Match either the username verbatim or the email (case-insensitively, via
	// the lowercased-email column). A username never contains '@', so the two
	// namespaces don't collide in practice.
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, username, email, email_verified_at, password_hash, is_instance_admin
		  FROM users
		 WHERE (username = ? OR email = ?) AND is_active = 1`,
		identifier, normalizeEmail(identifier),
	).Scan(&userID, &username, &email, &emailVerified, &hash, &isAdmin)

	userMissing := errors.Is(err, sql.ErrNoRows)
	if !userMissing && err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup user failed")
		return
	}
	if userMissing {
		hash = auth.DummyVerifyHash()
	}

	ok, _ := auth.VerifyPassword(req.Password, hash)
	if userMissing || !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}

	// Email accounts must confirm before they can sign in. Legacy/bootstrap
	// rows with no email are exempt.
	if email.Valid && !emailVerified.Valid {
		writeError(w, http.StatusForbidden, "email_unverified", "confirm your email before signing in")
		return
	}

	sid, err := auth.CreateSession(ctx, s.DB, userID, r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create session failed")
		return
	}
	auth.SetSessionCookie(w, sid)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": authUserDTO{
			ID:              userID,
			Username:        username,
			Email:           nullableString(email),
			EmailVerified:   emailVerified.Valid,
			IsInstanceAdmin: isAdmin == 1,
		},
	})
}

// Logout removes the current session row and clears the cookie. Always 204,
// even when no cookie is present — logout is idempotent.
func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil && c.Value != "" {
		_ = auth.DeleteSession(r.Context(), s.DB, c.Value)
	}
	auth.SetSessionCookie(w, "")
	w.WriteHeader(http.StatusNoContent)
}

// Me returns the currently authenticated user. Bypasses the middleware so it
// can answer the frontend's boot probe with a clean 401 envelope. Mirrors the
// error-class split from auth.Middleware: ErrInvalidSession → 401, every
// other DB error → 500 (so a transient backend failure doesn't evict the
// signed-in user).
func (s *Server) Me(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(auth.CookieName)
	if err != nil || c.Value == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}
	u, err := auth.LoadSessionAndSlide(r.Context(), s.DB, c.Value)
	if errors.Is(err, auth.ErrInvalidSession) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}
	if err != nil {
		log.Printf("auth.Me: session lookup failed: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	var email *string
	if u.Email != "" {
		email = &u.Email
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": authUserDTO{
			ID:       u.ID,
			Username: u.Username,
			Email:    email,
			// A live session implies the account cleared the login email gate
			// (or has no email at all), so an email here is a confirmed one.
			EmailVerified:   u.Email != "",
			IsInstanceAdmin: u.IsInstanceAdmin,
		},
	})
}
