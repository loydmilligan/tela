package api

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

// events.go — the unified activity feed (table `events`, migration 0033). Every
// noteworthy action calls recordEvent at its chokepoint; ListEvents reads them
// back for the instance-admin Events screen with filters + keyset pagination.
// Best-effort writes (mirrors writeAudit): a failed insert never fails the
// operation it records.

// Event type constants. access.* types are composed at the call site
// ("access." + audit action).
const (
	evtAuthLogin       = "auth.login"
	evtAuthLoginFailed = "auth.login_failed"
	evtAuthLogout      = "auth.logout"
	evtPageView        = "page.view"
	evtPageCreate      = "page.create"
	evtPageEdit        = "page.edit"
	evtAsk             = "ask"
	evtAPIRequest      = "api.request"
	evtClientError     = "client.error"
)

// eventInput is one row to append. Pointers are NULL when absent (anonymous
// actor, no target). Labels are denormalised so the feed renders join-free.
type eventInput struct {
	Type        string
	ActorUserID *int64
	ActorLabel  string
	TargetKind  string
	TargetID    *int64
	TargetLabel string
	Detail      string
	IP          string
	UserAgent   string
}

// recordEvent appends one row. Best-effort: errors are logged, never returned —
// activity logging must not break the action it records. `ex` is *sql.DB or a Tx.
func recordEvent(ctx context.Context, ex emailTokenExec, e eventInput) {
	// actor_label auto-fills from the user when the caller didn't supply one, so
	// audit-side callers don't need to thread the username through. An explicit
	// label still wins (e.g. the attempted identifier on a failed login, where no
	// user row exists).
	if _, err := ex.ExecContext(ctx, `
		INSERT INTO events (type, actor_user_id, actor_label, target_kind, target_id, target_label, detail, ip, user_agent)
		VALUES ($1, $2, COALESCE(NULLIF($3, ''), (SELECT username FROM users WHERE id = $2), ''), $4, $5, $6, $7, $8, $9)`,
		e.Type, e.ActorUserID, e.ActorLabel, e.TargetKind, e.TargetID, e.TargetLabel, e.Detail, e.IP, e.UserAgent,
	); err != nil {
		slog.Error("event write failed", "type", e.Type, "err", err)
	}
}

// recordRequestEvent fills actor (from the session context) + ip + user_agent
// from the request when the caller hasn't set them, then records. Use for events
// triggered by an authenticated/session request.
func (s *Server) recordRequestEvent(r *http.Request, e eventInput) {
	if e.ActorUserID == nil {
		if u, ok := auth.UserFromContext(r.Context()); ok {
			id := u.ID
			e.ActorUserID = &id
			if e.ActorLabel == "" {
				e.ActorLabel = u.Username
			}
		}
	}
	if e.IP == "" {
		e.IP = clientIPForRateLimit(r)
	}
	if e.UserAgent == "" {
		e.UserAgent = r.UserAgent()
	}
	recordEvent(r.Context(), s.DB, e)
}

type eventDTO struct {
	ID          int64   `json:"id"`
	Type        string  `json:"type"`
	ActorUserID *int64  `json:"actor_user_id"`
	ActorLabel  string  `json:"actor_label"`
	TargetKind  string  `json:"target_kind"`
	TargetID    *int64  `json:"target_id"`
	TargetLabel string  `json:"target_label"`
	Detail      string  `json:"detail"`
	IP          string  `json:"ip"`
	UserAgent   string  `json:"user_agent"`
	CreatedAt   string  `json:"created_at"`
}

// ListEvents — GET /api/admin/events. Instance-admin only. Filters: types (csv),
// user_id, q (free-text over actor/target/detail), since (date or datetime),
// before (keyset cursor = id). Returns newest-first with next_cursor for
// infinite scroll (null when the page wasn't full).
func (s *Server) ListEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireInstanceAdmin(w, r); !ok {
		return
	}
	q := r.URL.Query()
	limit := clampLimit(q.Get("limit"), 50, 200)

	conds := []string{}
	args := []any{}

	if raw := strings.TrimSpace(q.Get("types")); raw != "" {
		// A token ending in '.' is a family prefix (e.g. "access." matches every
		// access.<action>); others match exactly. The branches OR together.
		ors := []string{}
		exact := []string{}
		for _, t := range strings.Split(raw, ",") {
			if t = strings.TrimSpace(t); t == "" {
				continue
			}
			if strings.HasSuffix(t, ".") {
				args = append(args, t+"%")
				ors = append(ors, fmt.Sprintf("type LIKE $%d", len(args)))
			} else {
				args = append(args, t)
				exact = append(exact, fmt.Sprintf("$%d", len(args)))
			}
		}
		if len(exact) > 0 {
			ors = append(ors, "type IN ("+strings.Join(exact, ",")+")")
		}
		if len(ors) > 0 {
			conds = append(conds, "("+strings.Join(ors, " OR ")+")")
		}
	}
	if uid := q.Get("user_id"); uid != "" {
		if n, err := strconv.ParseInt(uid, 10, 64); err == nil {
			args = append(args, n)
			conds = append(conds, fmt.Sprintf("actor_user_id = $%d", len(args)))
		}
	}
	if since := strings.TrimSpace(q.Get("since")); since != "" {
		args = append(args, since)
		conds = append(conds, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if search := strings.TrimSpace(q.Get("q")); search != "" {
		args = append(args, "%"+search+"%")
		i := len(args)
		conds = append(conds, fmt.Sprintf("(actor_label ILIKE $%d OR target_label ILIKE $%d OR detail ILIKE $%d)", i, i, i))
	}
	if before := q.Get("before"); before != "" {
		if n, err := strconv.ParseInt(before, 10, 64); err == nil {
			args = append(args, n)
			conds = append(conds, fmt.Sprintf("id < $%d", len(args)))
		}
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, limit)

	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT id, type, actor_user_id, actor_label, target_kind, target_id, target_label, detail, ip, user_agent, created_at
		  FROM events `+where+`
		 ORDER BY id DESC
		 LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list events failed")
		return
	}
	defer rows.Close()

	events := []eventDTO{}
	for rows.Next() {
		var (
			e                  eventDTO
			actorID, targetID  sql.NullInt64
		)
		if err := rows.Scan(&e.ID, &e.Type, &actorID, &e.ActorLabel, &e.TargetKind, &targetID, &e.TargetLabel, &e.Detail, &e.IP, &e.UserAgent, &e.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan event row failed")
			return
		}
		if actorID.Valid {
			e.ActorUserID = &actorID.Int64
		}
		if targetID.Valid {
			e.TargetID = &targetID.Int64
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate events failed")
		return
	}

	// Full page → there may be more; hand back the last id as the keyset cursor.
	var nextCursor *int64
	if len(events) == limit {
		nextCursor = &events[len(events)-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "next_cursor": nextCursor})
}
