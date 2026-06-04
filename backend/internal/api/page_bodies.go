package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"
)

const (
	pageBodiesDefaultLimit = 200
	pageBodiesMaxLimit     = 500

	// sinceLayout matches the rest of the API's datetime-on-wire shape:
	// SQLite's datetime('now') output, UTC, no 'Z' suffix.
	sinceLayout = "2006-01-02 15:04:05"
)

// pageBodyDTO is the slim wire shape returned by the bodies endpoint. The
// palette tokenizes `body` client-side; no created_at, parent_id, or position
// — palette body search only needs (id, space, title, body, mtime).
type pageBodyDTO struct {
	ID        int64  `json:"id"`
	SpaceID   int64  `json:"space_id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	UpdatedAt string `json:"updated_at"`
}

// ListPageBodies returns slim page rows (id, space_id, title, body,
// updated_at) for a single space, optionally filtered to rows touched after a
// `since` watermark. Membership: any role on the space (viewer included) —
// palette body search MUST include readable pages, not only writable ones.
// Pagination uses ORDER BY id ASC with `cursor` = last returned id; the
// simpler id-cursor (vs. paired updated_at+id) is fine because the FE
// re-issues a fresh `?since=…` for delta refreshes rather than walking a
// cursor across mtime — and id ASC is monotonic for the lifetime of a row.
func (s *Server) ListPageBodies(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	spaceIDStr := q.Get("space_id")
	if spaceIDStr == "" {
		writeError(w, http.StatusBadRequest, "invalid_query", "space_id is required")
		return
	}
	spaceID, err := strconv.ParseInt(spaceIDStr, 10, 64)
	if err != nil || spaceID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_query", "space_id must be a positive integer")
		return
	}

	if err := verifySpaceExists(r.Context(), s.DB, spaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "space_not_found", "space not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "lookup space failed")
		return
	}
	if _, ok := s.requireMembership(w, r, spaceID); !ok {
		return
	}

	var sinceStr string
	if raw := q.Get("since"); raw != "" {
		if _, err := time.Parse(sinceLayout, raw); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "since must be 'YYYY-MM-DD HH:MM:SS' UTC")
			return
		}
		sinceStr = raw
	}

	cursor := int64(0)
	if raw := q.Get("cursor"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "cursor must be a non-negative integer")
			return
		}
		cursor = v
	}

	limit := int64(pageBodiesDefaultLimit)
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be a positive integer")
			return
		}
		if v > pageBodiesMaxLimit {
			v = pageBodiesMaxLimit
		}
		limit = v
	}

	// Fetch limit+1 so we can set has_more without a second roundtrip: if we
	// got back limit+1 rows there's at least one more page; drop the tail row
	// and use the last returned row's id as next_cursor.
	fetch := limit + 1

	args := []any{spaceID}
	stmt := `SELECT id, space_id, title, body, updated_at
	           FROM pages
	          WHERE space_id = $1`
	if sinceStr != "" {
		args = append(args, sinceStr)
		stmt += ` AND updated_at > $` + strconv.Itoa(len(args))
	}
	if cursor > 0 {
		args = append(args, cursor)
		stmt += ` AND id > $` + strconv.Itoa(len(args))
	}
	args = append(args, fetch)
	stmt += ` ORDER BY id ASC LIMIT $` + strconv.Itoa(len(args))

	rows, err := s.DB.QueryContext(r.Context(), stmt, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list page bodies failed")
		return
	}
	defer rows.Close()

	out := make([]pageBodyDTO, 0, limit)
	for rows.Next() {
		var p pageBodyDTO
		if err := rows.Scan(&p.ID, &p.SpaceID, &p.Title, &p.Body, &p.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan page body row failed")
			return
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate page bodies failed")
		return
	}

	hasMore := int64(len(out)) > limit
	var nextCursor any
	if hasMore {
		out = out[:limit]
		nextCursor = out[len(out)-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"pages":       out,
		"next_cursor": nextCursor,
		"has_more":    hasMore,
	})
}

// GetSpaceIndexVersion returns an opaque version string for the space's
// pages: MAX(updated_at) when at least one page exists, else the literal
// "empty". Treat as opaque on the wire — FE compares for equality only.
// Membership: any role on the space.
func (s *Server) GetSpaceIndexVersion(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := verifySpaceExists(r.Context(), s.DB, spaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "space_not_found", "space not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "lookup space failed")
		return
	}
	if _, ok := s.requireMembership(w, r, spaceID); !ok {
		return
	}

	var maxUpdated sql.NullString
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT MAX(updated_at) FROM pages WHERE space_id = $1`, spaceID,
	).Scan(&maxUpdated); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup index version failed")
		return
	}
	version := "empty"
	if maxUpdated.Valid && maxUpdated.String != "" {
		version = maxUpdated.String
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": version})
}
