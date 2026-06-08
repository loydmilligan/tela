package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/models"
)

// Space ownership transfer. A space's owning account (spaces.org_id) is set at
// creation; this lets an owner move an existing space to an org afterward (or
// back to personal). Mirrors the org-owned-creation rules: the human owner row
// is untouched, and the org gets editor access via a grant. Owner-only.

type transferSpaceRequest struct {
	// OrgID is the target org; null transfers the space back to personal
	// ownership (resolved via the space_members owner — the caller).
	OrgID *int64 `json:"org_id"`
}

// TransferSpace — POST /api/spaces/{id}/transfer.
func (s *Server) TransferSpace(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req transferSpaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	sp, ae := s.transferSpaceCore(r.Context(), u, k, id, req.OrgID)
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"space": sp})
}

func (s *Server) transferSpaceCore(ctx context.Context, u *auth.User, k *auth.APIKey, id int64, targetOrgID *int64) (models.Space, *apiErr) {
	role, ae := s.membershipCore(ctx, u, k, id)
	if ae != nil {
		return models.Space{}, ae
	}
	if role != roleOwner {
		return models.Space{}, &apiErr{http.StatusForbidden, "forbidden", "owner role required to transfer a space"}
	}
	// To an org: caller must be a member, and the org must have space quota.
	if targetOrgID != nil {
		if !u.IsInstanceAdmin {
			if _, err := orgRole(ctx, s.DB, u.ID, *targetOrgID); errors.Is(err, sql.ErrNoRows) {
				return models.Space{}, &apiErr{http.StatusForbidden, "forbidden", "not a member of the target org"}
			} else if err != nil {
				return models.Space{}, &apiErr{http.StatusInternalServerError, "internal", "lookup org membership failed"}
			}
		}
		if ae := s.checkSpaceQuota(ctx, account{Kind: accountOrg, ID: *targetOrgID}); ae != nil {
			return models.Space{}, ae
		}
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return models.Space{}, &apiErr{http.StatusInternalServerError, "internal", "begin tx failed"}
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE spaces SET org_id = $1 WHERE id = $2`, targetOrgID, id); err != nil {
		return models.Space{}, &apiErr{http.StatusInternalServerError, "internal", "transfer space failed"}
	}
	// An org-owned space is shared with the whole org as editors (idempotent —
	// the grant may already exist from a prior share).
	if targetOrgID != nil {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO space_grants(space_id, principal_kind, principal_id, role)
			 VALUES ($1, 'org', $2, 'editor') ON CONFLICT DO NOTHING`,
			id, *targetOrgID); err != nil {
			return models.Space{}, &apiErr{http.StatusInternalServerError, "internal", "share space with org failed"}
		}
	}
	if err := tx.Commit(); err != nil {
		return models.Space{}, &apiErr{http.StatusInternalServerError, "internal", "commit failed"}
	}

	detail := "personal"
	if targetOrgID != nil {
		detail = "org"
	}
	writeAudit(ctx, s.DB, &u.ID, "space.transfer", "space", id, detail)

	sp, err := selectSpaceByID(ctx, s.DB, id)
	if err != nil {
		return models.Space{}, &apiErr{http.StatusInternalServerError, "internal", "fetch space failed"}
	}
	return sp, nil
}
