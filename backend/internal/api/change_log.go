package api

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"

	"github.com/zcag/tela/backend/internal/auth"
)

// change_log.go — the per-space change feed (sync §4 D10). Every page write
// appends a row (atomically, in the same tx as the change) via appendChangeLog,
// called from the shared in-tx primitives so REST, MCP, and sync all feed it.
// GET /api/changes ranges over it by the monotonic `seq` cursor.

const (
	changesDefaultLimit = 200
	changesMaxLimit     = 1000
)

// change action labels (match the migration's CHECK constraint).
const (
	changeCreated = "created"
	changeUpdated = "updated"
	changeMoved   = "moved"
	changeDeleted = "deleted"
)

// appendChangeLog records one page change in the feed within the caller's open
// tx, so the log entry and the change commit together (a rolled-back write logs
// nothing). Called from createPageCore / applyUpdateTx / applyMoveTx /
// deletePageCore — every path that mutates a page.
func appendChangeLog(ctx context.Context, tx *sql.Tx, spaceID, pageID int64, action string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO change_log (space_id, page_id, action) VALUES ($1, $2, $3)`,
		spaceID, pageID, action)
	return err
}

// changeRow is one delta in the feed.
type changeRow struct {
	Seq       int64  `json:"seq"`
	PageID    int64  `json:"page_id"`
	Action    string `json:"action"`
	CreatedAt string `json:"created_at"`
}

// ListChanges (GET /api/changes?space_id&since&limit): the delta feed for a
// space. Session OR bearer-read; membership on space_id required. Returns
// changes with seq > since (ascending) plus a `cursor` to pass as the next
// `since`. A client polls this instead of re-PROPFINDing the whole tree.
func (s *Server) ListChanges(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	spaceID, err := strconv.ParseInt(r.URL.Query().Get("space_id"), 10, 64)
	if err != nil || spaceID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_space_id", "space_id query param is required")
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64) // absent/garbage → 0 = from the start
	if since < 0 {
		since = 0
	}
	limit := changesDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > changesMaxLimit {
		limit = changesMaxLimit
	}

	k, _ := auth.APIKeyFromContext(r.Context())
	if _, ae := s.membershipCore(r.Context(), u, k, spaceID); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}

	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT seq, page_id, action, created_at
		   FROM change_log WHERE space_id = $1 AND seq > $2
		  ORDER BY seq ASC LIMIT $3`, spaceID, since, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list changes failed")
		return
	}
	defer rows.Close()

	changes := []changeRow{}
	cursor := since
	for rows.Next() {
		var c changeRow
		if err := rows.Scan(&c.Seq, &c.PageID, &c.Action, &c.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan change row failed")
			return
		}
		changes = append(changes, c)
		cursor = c.Seq
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate changes failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"changes": changes, "cursor": cursor})
}
