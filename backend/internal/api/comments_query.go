package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
)

// Comment query — the comments twin of pages_query.go. A ` ```query ` block with
// `target: comments` (and the query_comments MCP tool) filters COMMENT props and
// returns comment rows, so a change logged as a structured comment can be pulled
// back into a live changelog table.
//
// Deliberately a sibling core rather than a `target` flag on queryPagesCore: the
// two return different row shapes (a comment carries body/author/page context),
// and Go's typed rows + the MCP typed output schema both want one concrete type
// per tool. The BLOCK still exposes one `query` surface with `target:` and routes
// to the right endpoint — unified authoring, typed backend.
//
// SECURITY: identical gate to pages_query — JOIN pages → the caller's
// space_access (docs/access-model.md invariant 4, one resolution path). A comment
// is only visible if its PAGE is readable, so this can never leak discussion from
// a space the caller can't read. Sort is whitelisted; limit is a bound int.

type commentsQueryRequest struct {
	// Where is the props containment filter (comments.props @> where).
	Where map[string]any `json:"where"`
	// Space scopes to one space (same shape as pagesQueryRequest.Space).
	Space any `json:"space"`
	// PageID scopes to a single page's comments — the changelog-footer case.
	PageID *int64 `json:"page_id"`
	// IncludeResolved keeps resolved comments in the result (default: hidden,
	// mirroring the comments panel's default).
	IncludeResolved bool `json:"include_resolved"`
	// Sort is a whitelisted key: (-)created | (-)updated.
	Sort  string `json:"sort"`
	Limit int    `json:"limit"`
}

type queryCommentRow struct {
	ID        int64          `json:"id"`
	PageID    int64          `json:"page_id"`
	PageTitle string         `json:"page_title"`
	SpaceID   int64          `json:"space_id"`
	SpaceName string         `json:"space_name"`
	Author    string         `json:"author_username"`
	Body      string         `json:"body"`
	Props     map[string]any `json:"props"`
	Resolved  bool           `json:"resolved"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

// commentSortSQL whitelists sort key → ORDER BY fragment, so the fragment is
// never built from user input. Default is chronological (a changelog reads
// oldest→newest is wrong for a feed; -created puts the newest change on top).
var commentSortSQL = map[string]string{
	"created":  "c.created_at ASC, c.id ASC",
	"-created": "c.created_at DESC, c.id DESC",
	"updated":  "c.updated_at ASC, c.id ASC",
	"-updated": "c.updated_at DESC, c.id DESC",
}

// QueryComments handles POST /api/comments/query — session-gated; the core
// re-gates every row through space_access.
func (s *Server) QueryComments(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req commentsQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	rows, ae := s.queryCommentsCore(r.Context(), u, k, req)
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": rows})
}

func (s *Server) queryCommentsCore(ctx context.Context, u *auth.User, k *auth.APIKey, req commentsQueryRequest) ([]queryCommentRow, *apiErr) {
	sortKey := strings.TrimSpace(req.Sort)
	if sortKey == "" {
		sortKey = "-created"
	}
	orderFrag, ok := commentSortSQL[sortKey]
	if !ok {
		return nil, &apiErr{http.StatusBadRequest, "bad_request", "unsupported sort key"}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = queryDefaultLimit
	}
	if limit > queryMaxLimit {
		limit = queryMaxLimit
	}

	// Reuse the pages resolver so "here"/id/all behave identically on both
	// targets — one space-scoping semantic, not two.
	spaceFilterID, ae := s.resolveQuerySpace(ctx, pagesQueryRequest{Space: req.Space, PageID: req.PageID})
	if ae != nil {
		return nil, ae
	}

	args := []any{u.ID, propsJSON(req.Where)}
	q := `SELECT c.id, c.page_id, p.title, p.space_id, s.name, author.username,
	             c.body, c.props, c.resolved, c.created_at, c.updated_at
	        FROM comments c
	        JOIN pages p ON p.id = c.page_id
	        JOIN spaces s ON s.id = p.space_id
	        JOIN users author ON author.id = c.author_id
	        JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm
	          ON sm.space_id = p.space_id
	       WHERE c.deleted_at IS NULL
	         AND p.deleted_at IS NULL
	         AND c.props @> $2::jsonb`
	if !req.IncludeResolved {
		q += ` AND c.resolved = 0`
	}
	if spaceFilterID != nil {
		args = append(args, *spaceFilterID)
		q += fmt.Sprintf(" AND p.space_id = $%d", len(args))
	}
	// A page-scoped query (the changelog footer) narrows to one page.
	if req.PageID != nil {
		args = append(args, *req.PageID)
		q += fmt.Sprintf(" AND c.page_id = $%d", len(args))
	}
	// An API-key-scoped caller (MCP PAT) is confined to its one space, matching
	// every other read.
	if k != nil && k.SpaceID != nil {
		args = append(args, *k.SpaceID)
		q += fmt.Sprintf(" AND p.space_id = $%d", len(args))
	}
	q += " ORDER BY " + orderFrag
	args = append(args, limit)
	q += fmt.Sprintf(" LIMIT $%d", len(args))

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "query comments failed"}
	}
	defer rows.Close()

	out := []queryCommentRow{}
	for rows.Next() {
		var row queryCommentRow
		var propsRaw []byte
		var resolvedInt int
		if err := rows.Scan(&row.ID, &row.PageID, &row.PageTitle, &row.SpaceID, &row.SpaceName,
			&row.Author, &row.Body, &propsRaw, &resolvedInt, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, &apiErr{http.StatusInternalServerError, "internal", "scan comment row failed"}
		}
		row.Resolved = resolvedInt != 0
		row.Props = map[string]any{}
		if len(propsRaw) > 0 {
			if err := json.Unmarshal(propsRaw, &row.Props); err != nil {
				return nil, &apiErr{http.StatusInternalServerError, "internal", "decode comment props failed"}
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "query comments iterate failed"}
	}
	return out, nil
}
