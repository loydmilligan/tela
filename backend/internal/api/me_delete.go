package api

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/zcag/tela/backend/internal/auth"
)

// DeleteMyAccount handles DELETE /api/users/me — GDPR right-to-erasure self-service.
//
// Safety check first: if the caller is the sole admin of any org, returns 422
// so they can transfer admin rights or delete the org before proceeding.
//
// On success (in a single transaction):
//  1. Delete all sessions, API keys, email tokens.
//  2. Remove space memberships where the user is NOT the sole owner.
//  3. Hard-delete spaces where they ARE the sole owner (same as DeleteSpace;
//     cascades pages/comments via FK).
//  4. Anonymise the user row: username → "deleted_{id}", email/bio/display_name
//     → NULL, is_active → 0, deleted_at stamped. The row is kept so page
//     authorship FKs don't break.
//
// After commit, best-effort cancel any active Polar subscription (async; errors
// are logged, not surfaced — the webhook will reconcile the cancellation).
func (s *Server) DeleteMyAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	// Safety: block if the user is the sole admin of any org.
	var isSoleOrgAdmin bool
	if err := s.DB.QueryRowContext(ctx, `
		SELECT EXISTS(
		    SELECT 1 FROM org_members om
		     WHERE om.user_id = $1 AND om.org_role = 'admin'
		       AND NOT EXISTS (
		           SELECT 1 FROM org_members om2
		            WHERE om2.org_id = om.org_id
		              AND om2.org_role = 'admin'
		              AND om2.user_id != $1
		       )
		)`, u.ID).Scan(&isSoleOrgAdmin); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "check org admin failed")
		return
	}
	if isSoleOrgAdmin {
		writeError(w, http.StatusUnprocessableEntity, "org_owner",
			"You are the only admin of one or more orgs. Transfer admin rights or delete the org before deleting your account.")
		return
	}

	// Collect sole-owner space IDs before touching anything.
	soleSpaceIDs, err := userSoleOwnerSpaces(ctx, s.DB, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "check space ownership failed")
		return
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	// 1. Wipe sessions.
	if err := auth.DeleteUserSessions(ctx, tx, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete sessions failed")
		return
	}
	// 2. Wipe API keys.
	if _, err := tx.ExecContext(ctx, `DELETE FROM api_keys WHERE user_id = $1`, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete api keys failed")
		return
	}
	// 3. Wipe email tokens.
	if _, err := tx.ExecContext(ctx, `DELETE FROM email_tokens WHERE user_id = $1`, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete email tokens failed")
		return
	}
	// 4. Remove space memberships where the user is NOT the sole owner.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM space_members
		 WHERE user_id = $1
		   AND space_id NOT IN (
		       SELECT space_id FROM space_members
		        WHERE role = 'owner' AND user_id = $1
		   )`, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "remove space memberships failed")
		return
	}
	// 5. Delete spaces where the user is the sole owner (cascades via FK).
	for _, spaceID := range soleSpaceIDs {
		// Clear polymorphic subscriptions first (no FK, so the cascade misses them).
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM subscriptions
			 WHERE (subject_kind = 'space' AND subject_id = $1)
			    OR (subject_kind = 'page'  AND subject_id IN (SELECT id FROM pages WHERE space_id = $1))`,
			spaceID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "delete space subscriptions failed")
			return
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM spaces WHERE id = $1`, spaceID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "delete space failed")
			return
		}
	}
	// 6. Anonymise the user row (keep for FK integrity; block future logins).
	if _, err := tx.ExecContext(ctx, `
		UPDATE users SET
		    username     = 'deleted_' || id,
		    email        = NULL,
		    bio          = NULL,
		    display_name = NULL,
		    is_active    = 0,
		    deleted_at   = tela_now(),
		    updated_at   = tela_now()
		 WHERE id = $1`, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "anonymise user failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}

	// 7. Cancel Polar subscription best-effort (outside the TX so a billing
	//    hiccup can't roll back the erasure).
	go s.cancelUserPolarSubscription(context.Background(), u.ID)

	// Clear the session cookie so the browser is signed out immediately.
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// userSoleOwnerSpaces returns the IDs of spaces where userID is the only owner.
func userSoleOwnerSpaces(ctx context.Context, db *sql.DB, userID int64) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT space_id FROM space_members
		 WHERE role = 'owner' AND user_id = $1
		   AND space_id NOT IN (
		       SELECT space_id FROM space_members
		        WHERE role = 'owner' AND user_id != $1
		   )`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// cancelUserPolarSubscription is best-effort: log the error if cancel fails;
// Polar's own subscription.revoked webhook will reconcile the plan when the
// customer portal cancellation eventually fires.
func (s *Server) cancelUserPolarSubscription(ctx context.Context, userID int64) {
	var subID string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(polar_subscription_id, '') FROM users WHERE id = $1`, userID).Scan(&subID); err != nil || subID == "" {
		return
	}
	if !s.billing.Enabled() {
		return
	}
	if err := s.billing.CancelSubscription(ctx, subID); err != nil {
		slog.Error("account delete: cancel polar subscription", "user_id", userID, "sub_id", subID, "err", err)
	}
}
