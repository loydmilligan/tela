package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

// Props query block. A ` ```query ` block in a page body renders a live table of
// pages filtered by their props (a Dataview analog). The frontend parses the
// block's YAML spec and POSTs it here; this returns the matching pages.
//
// Storage is ready-made: pages.props is JSONB with a GIN jsonb_path_ops index
// (migration 0005), so `props @> $1::jsonb` containment is indexed — no schema
// change. v1 is deliberately small: equality/containment only (no operators),
// a whitelisted sort, and a clamped limit.
//
// SECURITY: results are JOINed to the caller's space_access (docs/access-model.md
// invariant 4 — one resolution path), so a query block can never surface a page
// from a space the caller can't read. The sort column comes from a fixed map and
// the limit is a bound int, so neither is interpolated from user input.

type pagesQueryRequest struct {
	// Where is the props containment filter (props @> where). Empty → all pages.
	Where map[string]any `json:"where"`
	// Space scopes the query: "here" (resolve from PageID), a space id (JSON
	// number or numeric string), or absent/"all"/null → every readable space.
	Space any `json:"space"`
	// PageID is the page the block lives on — used only to resolve Space="here".
	PageID *int64 `json:"page_id"`
	// Sort is a whitelisted key: (-)updated | (-)created | (-)title.
	Sort string `json:"sort"`
	// Limit caps rows; clamped to [1, queryMaxLimit].
	Limit int `json:"limit"`
}

type queryPageRow struct {
	ID        int64          `json:"id"`
	SpaceID   int64          `json:"space_id"`
	SpaceName string         `json:"space_name"`
	Title     string         `json:"title"`
	Props     map[string]any `json:"props"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

const (
	queryDefaultLimit = 50
	queryMaxLimit     = 200
)

// querySortSQL whitelists the sort key → a fixed ORDER BY fragment. A "-" prefix
// is descending. A key not in this map is rejected (400), so the fragment is
// never built from user input. The id tiebreak keeps paging stable.
var querySortSQL = map[string]string{
	"updated":  "p.updated_at ASC, p.id ASC",
	"-updated": "p.updated_at DESC, p.id DESC",
	"created":  "p.created_at ASC, p.id ASC",
	"-created": "p.created_at DESC, p.id DESC",
	"title":    "p.title ASC, p.id ASC",
	"-title":   "p.title DESC, p.id DESC",
}

// QueryPages handles POST /api/pages/query — session-gated; the core re-gates
// every row through space_access.
func (s *Server) QueryPages(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req pagesQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	rows, ae := s.queryPagesCore(r.Context(), u, k, req)
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": rows})
}

func (s *Server) queryPagesCore(ctx context.Context, u *auth.User, k *auth.APIKey, req pagesQueryRequest) ([]queryPageRow, *apiErr) {
	// Sort whitelist. Empty → default (newest first). Unknown → 400 so a typo is
	// loud rather than silently sorting by something else.
	sortKey := strings.TrimSpace(req.Sort)
	if sortKey == "" {
		sortKey = "-updated"
	}
	orderFrag, ok := querySortSQL[sortKey]
	if !ok {
		return nil, &apiErr{http.StatusBadRequest, "bad_request", "unsupported sort key"}
	}

	// Limit clamp.
	limit := req.Limit
	if limit <= 0 {
		limit = queryDefaultLimit
	}
	if limit > queryMaxLimit {
		limit = queryMaxLimit
	}

	// Space scope.
	spaceFilterID, ae := s.resolveQuerySpace(ctx, req)
	if ae != nil {
		return nil, ae
	}

	// props @> where containment. Empty where marshals to "{}", matching every
	// page (containment of the empty object is always true).
	whereJSON := propsJSON(req.Where)

	args := []any{u.ID, whereJSON}
	q := `SELECT p.id, p.space_id, s.name, p.title, p.props, p.created_at, p.updated_at
	        FROM pages p
	        JOIN spaces s ON s.id = p.space_id
	        JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm
	          ON sm.space_id = p.space_id
	       WHERE p.deleted_at IS NULL
	         AND p.props @> $2::jsonb`
	if spaceFilterID != nil {
		args = append(args, *spaceFilterID)
		q += fmt.Sprintf(" AND p.space_id = $%d", len(args))
	}
	// An API-key-scoped caller (MCP PAT) is confined to its one space, matching
	// every other page read.
	if k != nil && k.SpaceID != nil {
		args = append(args, *k.SpaceID)
		q += fmt.Sprintf(" AND p.space_id = $%d", len(args))
	}
	q += " ORDER BY " + orderFrag
	args = append(args, limit)
	q += fmt.Sprintf(" LIMIT $%d", len(args))

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "query pages failed"}
	}
	defer rows.Close()

	out := []queryPageRow{}
	for rows.Next() {
		var row queryPageRow
		var propsRaw []byte
		if err := rows.Scan(&row.ID, &row.SpaceID, &row.SpaceName, &row.Title, &propsRaw, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, &apiErr{http.StatusInternalServerError, "internal", "scan query row failed"}
		}
		row.Props = map[string]any{}
		if len(propsRaw) > 0 {
			if err := json.Unmarshal(propsRaw, &row.Props); err != nil {
				return nil, &apiErr{http.StatusInternalServerError, "internal", "decode props failed"}
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "query pages iterate failed"}
	}
	return out, nil
}

// resolveQuerySpace turns the request's Space field into an optional space-id
// filter: nil means "every readable space" (the space_access join still gates).
func (s *Server) resolveQuerySpace(ctx context.Context, req pagesQueryRequest) (*int64, *apiErr) {
	switch v := req.Space.(type) {
	case nil:
		return nil, nil
	case float64: // JSON numbers decode to float64
		id := int64(v)
		return &id, nil
	case int64: // an in-process caller (MCP query_pages) passes a native id
		return &v, nil
	case string:
		sv := strings.TrimSpace(strings.ToLower(v))
		switch {
		case sv == "" || sv == "all":
			return nil, nil
		case sv == "here":
			if req.PageID == nil {
				return nil, &apiErr{http.StatusBadRequest, "bad_request", "space: here needs a page context"}
			}
			var sid int64
			err := s.DB.QueryRowContext(ctx,
				`SELECT space_id FROM pages WHERE id = $1 AND deleted_at IS NULL`, *req.PageID).Scan(&sid)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, &apiErr{http.StatusNotFound, "not_found", "page not found"}
			}
			if err != nil {
				return nil, &apiErr{http.StatusInternalServerError, "internal", "resolve space failed"}
			}
			return &sid, nil
		default:
			if id, perr := strconv.ParseInt(sv, 10, 64); perr == nil {
				return &id, nil
			}
			return nil, &apiErr{http.StatusBadRequest, "bad_request", "invalid space"}
		}
	default:
		return nil, &apiErr{http.StatusBadRequest, "bad_request", "invalid space"}
	}
}
