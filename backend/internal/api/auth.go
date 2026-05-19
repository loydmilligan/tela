package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/zcag/tela/backend/internal/auth"
)

type authLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authUserDTO struct {
	ID              int64  `json:"id"`
	Username        string `json:"username"`
	IsInstanceAdmin bool   `json:"is_instance_admin"`
}

// Login authenticates a username/password pair. On success it creates a
// session, sets the canonical cookie, and returns the user. On failure it
// returns a single generic 401 envelope — no distinction between bad-user
// and bad-password, and the argon2id verify still runs against a dummy hash
// when the user is missing to keep response time roughly constant.
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var req authLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}

	ctx := r.Context()
	var (
		userID  int64
		hash    string
		isAdmin int
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, password_hash, is_instance_admin
		  FROM users
		 WHERE username = ? AND is_active = 1`, req.Username,
	).Scan(&userID, &hash, &isAdmin)

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

	sid, err := auth.CreateSession(ctx, s.DB, userID, r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create session failed")
		return
	}
	auth.SetSessionCookie(w, sid)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": authUserDTO{
			ID:              userID,
			Username:        req.Username,
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
	writeJSON(w, http.StatusOK, map[string]any{
		"user": authUserDTO{
			ID:              u.ID,
			Username:        u.Username,
			IsInstanceAdmin: u.IsInstanceAdmin,
		},
	})
}
