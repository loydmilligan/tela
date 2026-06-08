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
	"github.com/zcag/tela/backend/internal/mailer"
)

type registerRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Register creates a new, unconfirmed account and emails a verification link.
// Self-service signup is open: anyone with a valid email may register. The
// account cannot sign in until the link is followed (see Login's email gate).
// Duplicate email/username return 409 — for an open team wiki, a clear error
// beats enumeration-resistance.
func (s *Server) Register(w http.ResponseWriter, r *http.Request) {
	if !s.allowAuth(w, r, "register") {
		return
	}
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	email := normalizeEmail(req.Email)
	username := strings.TrimSpace(req.Username)
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

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "hash password failed")
		return
	}

	ctx := r.Context()
	var userID int64
	// Auto-apply a 30-day trial of the paid personal tier. planFor resolves the
	// effective plan from trial_ends_at, so this needs no billing engine and
	// downgrades gracefully to personal_free at expiry.
	err = s.DB.QueryRowContext(ctx, `
		INSERT INTO users (username, email, password_hash, is_instance_admin, is_active,
			trial_plan_key, trial_ends_at)
		VALUES ($1, $2, $3, 0, 1, 'personal_plus',
			to_char((now() AT TIME ZONE 'UTC') + interval '30 days', 'YYYY-MM-DD HH24:MI:SS'))
		RETURNING id`, username, email, hash).Scan(&userID)
	if err != nil {
		if isUniqueConstraintErr(err) {
			// Either the username or the email collided. The message stays
			// generic so we don't confirm which one exists.
			writeError(w, http.StatusConflict, "conflict", "that email or username is already taken")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "create user failed")
		return
	}

	// Mint + send the confirmation link. A send failure is logged but does not
	// fail the request — the account exists and the user can use "resend".
	s.sendVerification(ctx, userID, username, email)

	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "email": email})
}

type verifyEmailRequest struct {
	Token string `json:"token"`
}

// VerifyEmail consumes a verification token, marks the account confirmed,
// provisions its personal space, and signs the user in (sets the session
// cookie) so confirmation lands them straight in the app.
func (s *Server) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "token is required")
		return
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	userID, err := consumeEmailToken(ctx, tx, "verify", req.Token)
	if errors.Is(err, errTokenInvalid) {
		writeError(w, http.StatusBadRequest, "invalid_token", "this confirmation link is invalid or has expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "consume token failed")
		return
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE users SET email_verified_at = COALESCE(email_verified_at, tela_now()),
		                 updated_at = tela_now()
		 WHERE id = $1`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "confirm email failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}

	// Provision the personal space now that the account is real (idempotent).
	var username, email string
	if err := s.DB.QueryRowContext(ctx, `SELECT username, COALESCE(email, '') FROM users WHERE id = $1`, userID).Scan(&username, &email); err == nil {
		if _, err := EnsurePersonalSpace(ctx, s.DB, userID, username); err != nil {
			log.Printf("personal space for verified user %d (%s): %v", userID, username, err)
		}
		// Enroll into any org whose auto-join domain matches the just-confirmed
		// address (#153).
		applyAutoJoin(ctx, s.DB, userID, email)
	}

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
	writeJSON(w, http.StatusOK, map[string]any{"user": dto})
}

type emailOnlyRequest struct {
	Email string `json:"email"`
}

// ResendVerification re-sends a confirmation link for an unverified account.
// Always 202 regardless of whether the address exists or is already verified,
// so it can't be used to probe which emails are registered.
func (s *Server) ResendVerification(w http.ResponseWriter, r *http.Request) {
	if !s.allowAuth(w, r, "resend") {
		return
	}
	var req emailOnlyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	email := normalizeEmail(req.Email)
	ctx := r.Context()

	var (
		userID   int64
		username string
		verified sql.NullString
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, username, email_verified_at FROM users
		 WHERE email = $1 AND is_active = 1`, email).Scan(&userID, &username, &verified)
	if err == nil && !verified.Valid {
		s.sendVerification(ctx, userID, username, email)
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Printf("resend verification lookup: %v", err)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// RequestPasswordReset emails a reset link. Always 202 (no enumeration); the
// link is only minted+sent when an active account owns the address.
func (s *Server) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	if !s.allowAuth(w, r, "forgot") {
		return
	}
	var req emailOnlyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	email := normalizeEmail(req.Email)
	ctx := r.Context()

	var (
		userID   int64
		username string
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, username FROM users
		 WHERE email = $1 AND is_active = 1`, email).Scan(&userID, &username)
	if err == nil {
		raw, terr := createEmailToken(ctx, s.DB, userID, "reset", resetTokenTTL)
		if terr != nil {
			log.Printf("create reset token for %d: %v", userID, terr)
		} else if serr := s.Mailer.Send(ctx, mailer.ResetPassword(email, username, resetLink(raw))); serr != nil {
			log.Printf("send reset email to %s: %v", email, serr)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("password reset lookup: %v", err)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// ResetPassword consumes a reset token, sets the new password, confirms the
// email if it wasn't already (following the link proves ownership), and wipes
// every session for the account so a stolen old session can't survive a reset.
func (s *Server) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "token is required")
		return
	}
	if len(req.Password) < minPasswordLen {
		writeError(w, http.StatusBadRequest, "bad_request", "password must be at least 8 characters")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "hash password failed")
		return
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	userID, err := consumeEmailToken(ctx, tx, "reset", req.Token)
	if errors.Is(err, errTokenInvalid) {
		writeError(w, http.StatusBadRequest, "invalid_token", "this reset link is invalid or has expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "consume token failed")
		return
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE users SET password_hash = $1,
		                 email_verified_at = COALESCE(email_verified_at, tela_now()),
		                 updated_at = tela_now()
		 WHERE id = $2`, hash, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "update password failed")
		return
	}
	if err := auth.DeleteUserSessions(ctx, tx, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "clear sessions failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// sendVerification mints a verify token for userID and emails the link.
// Best-effort: failures are logged, not surfaced — the caller has already
// committed the user row and "resend" exists as a recovery path.
func (s *Server) sendVerification(ctx context.Context, userID int64, username, email string) {
	raw, err := createEmailToken(ctx, s.DB, userID, "verify", verifyTokenTTL)
	if err != nil {
		log.Printf("create verify token for %d: %v", userID, err)
		return
	}
	if err := s.Mailer.Send(ctx, mailer.VerifyEmail(email, username, verifyLink(raw))); err != nil {
		log.Printf("send verify email to %s: %v", email, err)
	}
}

// allowAuth enforces the per-IP rate limit on the email-sending auth
// endpoints. Returns false (and writes 429) when the caller is over budget.
func (s *Server) allowAuth(w http.ResponseWriter, r *http.Request, purpose string) bool {
	ok, retry := s.authLimiter.allow(purpose, clientIPForRateLimit(r))
	if !ok {
		secs := int(retry.Seconds())
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later")
		return false
	}
	return true
}

// authUserByID loads the wire DTO for a freshly-authenticated user (used by
// the verify flow, which signs the user in).
func (s *Server) authUserByID(ctx context.Context, id int64) (authUserDTO, error) {
	var (
		dto      authUserDTO
		email    sql.NullString
		verified sql.NullString
		isAdmin  int
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, username, email, email_verified_at, is_instance_admin
		  FROM users WHERE id = $1`, id).
		Scan(&dto.ID, &dto.Username, &email, &verified, &isAdmin)
	if err != nil {
		return authUserDTO{}, err
	}
	dto.Email = nullableString(email)
	dto.EmailVerified = verified.Valid
	dto.IsInstanceAdmin = isAdmin == 1
	return dto, nil
}
