package api

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/zcag/tela/backend/internal/auth"
)

// access_audit trail for org / membership / grant / auto-join / domain changes
// (see docs/access-model.md). Writes are best-effort: a failed audit insert is
// logged but never fails the operation it records. Two read surfaces:
// ListAccessAudit (instance-admin, whole instance) and ListOrgAudit (org-admin,
// scoped to one org's rows).

// accessAuditEntry is the wire shape for the read endpoint. ActorUsername is joined
// from users; nil for system actions (auto-join).
type accessAuditEntry struct {
	ID            int64   `json:"id"`
	ActorUserID   *int64  `json:"actor_user_id"`
	ActorUsername *string `json:"actor_username"`
	Action        string  `json:"action"`
	TargetKind    string  `json:"target_kind"`
	TargetID      *int64  `json:"target_id"`
	Detail        string  `json:"detail"`
	CreatedAt     string  `json:"created_at"`
}

// writeAudit inserts one audit row. actorID nil = system action.
func writeAudit(ctx context.Context, ex emailTokenExec, actorID *int64, action, targetKind string, targetID int64, detail string) {
	if _, err := ex.ExecContext(ctx,
		`INSERT INTO access_audit (actor_user_id, action, target_kind, target_id, detail)
		 VALUES ($1, $2, $3, $4, $5)`,
		actorID, action, targetKind, targetID, detail); err != nil {
		slog.Error("audit write failed", "action", action, "err", err)
	}
	// Mirror into the unified events feed (access.<action>). access_audit stays
	// the canonical, un-GC'd access trail; this is the row the Events screen reads.
	var tid *int64
	if targetID != 0 {
		tid = &targetID
	}
	recordEvent(ctx, ex, eventInput{
		Type:        "access." + action,
		ActorUserID: actorID,
		TargetKind:  targetKind,
		TargetID:    tid,
		Detail:      detail,
	})
}

// audit records an action performed by the request's authenticated user.
func (s *Server) audit(ctx context.Context, r *http.Request, action, targetKind string, targetID int64, detail string) {
	var actorID *int64
	if u, ok := auth.UserFromContext(r.Context()); ok {
		actorID = &u.ID
	}
	writeAudit(ctx, s.DB, actorID, action, targetKind, targetID, detail)
}

// ListAccessAudit returns the most recent audit rows across the whole instance.
// Instance-admin only.
func (s *Server) ListAccessAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	limit := clampLimit(r.URL.Query().Get("limit"), 100, 200)
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT a.id, a.actor_user_id, u.username, a.action, a.target_kind, a.target_id, a.detail, a.created_at
		  FROM access_audit a
		  LEFT JOIN users u ON u.id = a.actor_user_id
		 ORDER BY a.id DESC
		 LIMIT $1`, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list audit failed")
		return
	}
	writeAuditRows(w, rows)
}

// ListOrgAudit returns the audit rows scoped to one org — membership, group,
// grant and domain changes for that tenant. Org-admin gated (instance-admins
// pass via requireOrgAdmin). Every org-scoped event is written with
// target_kind='org' + target_id=orgID, so a single filter covers the lot,
// giving a company's own admin the "who added whom / who shared what" view
// without exposing the rest of the instance.
func (s *Server) ListOrgAudit(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if !s.requireOrgAdmin(w, r, orgID) {
		return
	}
	// The org-scoped audit log is an Enterprise feature (entitled via plan on
	// cloud, or a license key on self-host).
	if !s.entitled(r.Context(), account{Kind: accountOrg, ID: orgID}, "audit") {
		writeError(w, http.StatusPaymentRequired, "upgrade_required", "the audit log is an Enterprise feature — move this org to the Enterprise plan to view it")
		return
	}
	limit := clampLimit(r.URL.Query().Get("limit"), 100, 200)
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT a.id, a.actor_user_id, u.username, a.action, a.target_kind, a.target_id, a.detail, a.created_at
		  FROM access_audit a
		  LEFT JOIN users u ON u.id = a.actor_user_id
		 WHERE a.target_kind = 'org' AND a.target_id = $1
		 ORDER BY a.id DESC
		 LIMIT $2`, orgID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list org audit failed")
		return
	}
	writeAuditRows(w, rows)
}

// writeAuditRows scans an access_audit result set into the wire shape and writes
// the JSON envelope. Closes rows. Shared by the instance- and org-scoped reads.
func writeAuditRows(w http.ResponseWriter, rows *sql.Rows) {
	defer rows.Close()
	entries := []accessAuditEntry{}
	for rows.Next() {
		var (
			e        accessAuditEntry
			actorID  sql.NullInt64
			username sql.NullString
			targetID sql.NullInt64
		)
		if err := rows.Scan(&e.ID, &actorID, &username, &e.Action, &e.TargetKind, &targetID, &e.Detail, &e.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan audit row failed")
			return
		}
		if actorID.Valid {
			e.ActorUserID = &actorID.Int64
		}
		e.ActorUsername = nullableString(username)
		if targetID.Valid {
			e.TargetID = &targetID.Int64
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate audit failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

// clampLimit parses a query-string limit, falling back to def and capping at
// max. Shared by audit (and available to other paginated reads).
func clampLimit(raw string, def, max int) int {
	n := def
	if raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			n = v
		}
	}
	if n > max {
		n = max
	}
	return n
}
