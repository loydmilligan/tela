package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/zcag/tela/backend/internal/mailer"
)

// Email invitations to an organization — the self-serve onboarding path. An org
// admin invites a teammate by email; the raw token lives only in the emailed
// link (we persist its SHA-256 hash, mirroring email_tokens). The invitee joins
// by accepting while logged in with the matching verified email, or auto-joins
// when they verify a fresh signup (applyPendingInvites, beside applyAutoJoin).

const inviteTokenTTL = 14 * 24 * time.Hour

type orgInviteDTO struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	OrgRole   string `json:"org_role"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

// CreateOrgInvite invites an email to the org (org admin). Re-inviting refreshes
// the pending invite. The seat limit is enforced at accept time, not here, so an
// admin can queue invites before upgrading.
func (s *Server) CreateOrgInvite(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	var req struct {
		Email   string `json:"email"`
		OrgRole string `json:"org_role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	email := normalizeEmail(req.Email)
	if !validEmail(email) {
		writeError(w, http.StatusBadRequest, "bad_request", "a valid email is required")
		return
	}
	role := req.OrgRole
	if role == "" {
		role = orgRoleMember
	}
	if role != orgRoleAdmin && role != orgRoleMember {
		writeError(w, http.StatusBadRequest, "bad_request", "org_role must be 'admin' or 'member'")
		return
	}
	ctx := r.Context()

	// Already a member? Then there's nothing to invite.
	var dummy int64
	memberErr := s.DB.QueryRowContext(ctx, `
		SELECT u.id FROM users u
		  JOIN org_members m ON m.user_id = u.id
		 WHERE m.org_id = $1 AND lower(u.email) = $2`, orgID, email).Scan(&dummy)
	if memberErr == nil {
		writeError(w, http.StatusConflict, "already_member", "that person is already in this organization")
		return
	}
	if !errors.Is(memberErr, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "internal", "membership check failed")
		return
	}

	var orgName string
	if err := s.DB.QueryRowContext(ctx, `SELECT name FROM orgs WHERE id = $1`, orgID).Scan(&orgName); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "org not found")
		return
	}

	raw, hash, err := newEmailToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "token generation failed")
		return
	}
	expires := time.Now().UTC().Add(inviteTokenTTL).Format("2006-01-02 15:04:05")

	// Refresh any outstanding invite for this (org, email) then insert — cleaner
	// than ON CONFLICT against the partial unique index.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM org_invites WHERE org_id = $1 AND lower(email) = $2 AND accepted_at IS NULL`, orgID, email); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "invite refresh failed")
		return
	}
	var inviteID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO org_invites (org_id, email, org_role, token_hash, invited_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		orgID, email, role, hash, u.ID, expires).Scan(&inviteID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create invite failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}

	// Send the branded invitation (best-effort — the invite exists regardless; if
	// mail is logging-only the link is in the logs, same as verify/reset).
	inviteURL := s.linkOrigin(r) + "/invite/" + raw
	inviter := u.Username
	if err := s.Mailer.Send(ctx, mailer.OrgInvite(email, orgName, inviter, inviteURL, s.emailBrandForRequest(r))); err != nil {
		// Don't fail the request — surface it in logs; the admin can re-send.
		writeAudit(ctx, s.DB, &u.ID, "org_invite.mail_failed", "org", orgID, email)
	}
	s.audit(ctx, r, "org_invite.create", "org", orgID, email)
	writeJSON(w, http.StatusCreated, map[string]any{"invite": orgInviteDTO{
		ID: inviteID, Email: email, OrgRole: role, ExpiresAt: expires,
	}})
}

// ListOrgInvites returns the org's pending (unaccepted, unexpired) invites. Org admin.
func (s *Server) ListOrgInvites(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT id, email, org_role, created_at, expires_at
		  FROM org_invites
		 WHERE org_id = $1 AND accepted_at IS NULL AND expires_at > tela_now()
		 ORDER BY created_at DESC`, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list invites failed")
		return
	}
	defer rows.Close()
	out := []orgInviteDTO{}
	for rows.Next() {
		var d orgInviteDTO
		if err := rows.Scan(&d.ID, &d.Email, &d.OrgRole, &d.CreatedAt, &d.ExpiresAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan invite failed")
			return
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": out})
}

// RevokeOrgInvite deletes a pending invite. Org admin.
func (s *Server) RevokeOrgInvite(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	inviteID, err := strconv.ParseInt(r.PathValue("inviteId"), 10, 64)
	if err != nil || inviteID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid invite id")
		return
	}
	res, err := s.DB.ExecContext(r.Context(),
		`DELETE FROM org_invites WHERE id = $1 AND org_id = $2 AND accepted_at IS NULL`, inviteID, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "revoke invite failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "no pending invite with that id")
		return
	}
	s.audit(r.Context(), r, "org_invite.revoke", "org", orgID, "")
	w.WriteHeader(http.StatusNoContent)
}

// GetInvite returns an invite's org + target email for the accept page. PUBLIC
// (under /api/invites/, bypasses session middleware) — it self-authenticates via
// the unguessable token, so a logged-out invitee can render the page before
// signing up. Never reveals anything the token holder doesn't already know.
func (s *Server) GetInvite(w http.ResponseWriter, r *http.Request) {
	hash := hashEmailToken(r.PathValue("token"))
	var (
		orgID   int64
		email   string
		orgName string
	)
	err := s.DB.QueryRowContext(r.Context(), `
		SELECT i.org_id, i.email, o.name
		  FROM org_invites i JOIN orgs o ON o.id = i.org_id
		 WHERE i.token_hash = $1 AND i.accepted_at IS NULL AND i.expires_at > tela_now()`,
		hash).Scan(&orgID, &email, &orgName)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "load invite failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "org_name": orgName, "email": email})
}

// AcceptInvite joins the logged-in user to the org named by the token. The
// caller's verified email must match the invite (invites are email-targeted, so
// a forwarded link can't enrol the wrong person). Seat-quota gated.
func (s *Server) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	ctx := r.Context()
	hash := hashEmailToken(req.Token)

	var (
		inviteID int64
		orgID    int64
		email    string
		role     string
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, org_id, email, org_role FROM org_invites
		 WHERE token_hash = $1 AND accepted_at IS NULL AND expires_at > tela_now()`,
		hash).Scan(&inviteID, &orgID, &email, &role)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "invalid_token", "this invitation is invalid or has expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "load invite failed")
		return
	}
	if normalizeEmail(u.Email) != normalizeEmail(email) {
		writeError(w, http.StatusForbidden, "email_mismatch", "this invitation is for "+email+" — sign in with that address to accept it")
		return
	}
	if ae := s.checkSeatQuota(ctx, orgID); ae != nil {
		writeError(w, ae.Status, ae.Code, "this organization is at its seat limit — ask an admin to add a seat")
		return
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO org_members (org_id, user_id, org_role) VALUES ($1, $2, $3)
		 ON CONFLICT (org_id, user_id) DO NOTHING`, orgID, u.ID, role); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "join org failed")
		return
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE org_invites SET accepted_at = tela_now() WHERE id = $1`, inviteID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "mark invite accepted failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	writeAudit(ctx, s.DB, &u.ID, "org_member.invite_accept", "org", orgID, email)
	org, err := selectOrgByID(ctx, s.DB, orgID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"org": org})
}

// applyPendingInvites enrols userID into every org with a pending invite for
// their just-verified email — the auto-join twin of applyAutoJoin, called from
// VerifyEmail so a fresh signup that arrived via an invite link lands in the
// team without a second click. Seat-blocked invites are left pending (the
// invitee can still accept later once the org frees a seat).
func (s *Server) applyPendingInvites(ctx context.Context, userID int64, email string) {
	email = normalizeEmail(email)
	if email == "" {
		return
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, org_id, org_role FROM org_invites
		 WHERE lower(email) = $1 AND accepted_at IS NULL AND expires_at > tela_now()`, email)
	if err != nil {
		slog.Error("pending invites lookup", "user_id", userID, "err", err)
		return
	}
	type pending struct {
		id, orgID int64
		role      string
	}
	var list []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.orgID, &p.role); err == nil {
			list = append(list, p)
		}
	}
	rows.Close()

	for _, p := range list {
		if ae := s.checkSeatQuota(ctx, p.orgID); ae != nil {
			continue // org full — leave the invite pending for a later manual accept
		}
		if _, err := s.DB.ExecContext(ctx,
			`INSERT INTO org_members (org_id, user_id, org_role) VALUES ($1, $2, $3)
			 ON CONFLICT (org_id, user_id) DO NOTHING`, p.orgID, userID, p.role); err != nil {
			slog.Error("pending invite join", "user_id", userID, "org_id", p.orgID, "err", err)
			continue
		}
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE org_invites SET accepted_at = tela_now() WHERE id = $1`, p.id); err != nil {
			slog.Error("pending invite mark", "invite_id", p.id, "err", err)
			continue
		}
		writeAudit(ctx, s.DB, &userID, "org_member.invite_accept", "org", p.orgID, email)
	}
}
