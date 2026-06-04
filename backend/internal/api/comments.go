package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/models"
)

const maxCommentBodyLen = 10_000

// commentThread bundles a root comment with its replies in created_at ASC
// order. GET /api/pages/{id}/comments returns []commentThread.
type commentThread struct {
	Root    models.Comment   `json:"root"`
	Replies []models.Comment `json:"replies"`
}

type commentCreateRequest struct {
	Body         string  `json:"body"`
	ParentID     *int64  `json:"parent_id"`
	AnchorPrefix *string `json:"anchor_prefix"`
	AnchorExact  *string `json:"anchor_exact"`
	AnchorSuffix *string `json:"anchor_suffix"`
}

// commentPatchRequest is mutually exclusive: exactly one of Body / Resolved
// may be set. The handler 400s when both fields are present.
type commentPatchRequest struct {
	Body     *string `json:"body"`
	Resolved *bool   `json:"resolved"`
}

// ListComments returns all threads for a page. Viewers get 403 (the comments
// surface does not exist for viewers, per the M8 doctrine — not an empty
// array). Resolved threads are included only when ?include_resolved=true.
func (s *Server) ListComments(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	page, err := selectPageByID(ctx, s.DB, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
		return
	}
	if !enforceAPIKeySpaceScope(w, r, page.SpaceID) {
		return
	}
	role, err := spaceRole(ctx, s.DB, u.ID, page.SpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup membership failed")
		return
	}
	if !canEdit(role) {
		writeError(w, http.StatusForbidden, "forbidden", "editor or owner role required")
		return
	}

	includeResolved := r.URL.Query().Get("include_resolved") == "true"

	rows, err := s.DB.QueryContext(ctx, commentSelectColumns+`
		  FROM comments c
		  JOIN users author ON author.id = c.author_id
		  LEFT JOIN users resolver ON resolver.id = c.resolved_by
		 WHERE c.page_id = $1 AND c.deleted_at IS NULL
		 ORDER BY c.created_at ASC, c.id ASC`, pageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list comments failed")
		return
	}
	defer rows.Close()

	all := []models.Comment{}
	for rows.Next() {
		c, err := scanCommentFromRows(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan comment row failed")
			return
		}
		all = append(all, c)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate comments failed")
		return
	}

	// Bucket replies onto their root. Replies whose root was soft-deleted
	// fall out (root row excluded by WHERE clause above, bucket lookup fails).
	byRoot := map[int64]*commentThread{}
	threads := []commentThread{}
	for _, c := range all {
		if c.ParentID == nil {
			if !includeResolved && c.Resolved {
				continue
			}
			threads = append(threads, commentThread{Root: c, Replies: []models.Comment{}})
			byRoot[c.ID] = &threads[len(threads)-1]
		}
	}
	for _, c := range all {
		if c.ParentID == nil {
			continue
		}
		t, ok := byRoot[*c.ParentID]
		if !ok {
			continue
		}
		t.Replies = append(t.Replies, c)
	}

	writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
}

// CreateComment inserts either a root (parent_id null, all three anchor_*
// required) or a reply (parent_id of a root in the same page, anchor_*
// ignored). Editor+ on the space required.
func (s *Server) CreateComment(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req commentCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "body is required")
		return
	}
	if len(body) > maxCommentBodyLen {
		writeError(w, http.StatusBadRequest, "bad_request", "body exceeds 10000 characters")
		return
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	page, err := selectPageByIDTx(ctx, tx, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
		return
	}
	if !enforceAPIKeySpaceScope(w, r, page.SpaceID) {
		return
	}
	role, err := spaceRoleTx(ctx, tx, u.ID, page.SpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup membership failed")
		return
	}
	if !canEdit(role) {
		writeError(w, http.StatusForbidden, "forbidden", "editor or owner role required")
		return
	}

	isReply := req.ParentID != nil
	var (
		anchorPrefix, anchorExact, anchorSuffix any // sql NULL when reply
	)

	if isReply {
		if *req.ParentID <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "parent_id must be a positive integer")
			return
		}
		var parentPageID int64
		var parentParentID sql.NullInt64
		var parentDeleted sql.NullString
		err := tx.QueryRowContext(ctx,
			`SELECT page_id, parent_id, deleted_at FROM comments WHERE id = $1`, *req.ParentID).
			Scan(&parentPageID, &parentParentID, &parentDeleted)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "comment_not_found", "parent comment not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "lookup parent comment failed")
			return
		}
		if parentDeleted.Valid {
			writeError(w, http.StatusNotFound, "comment_not_found", "parent comment not found")
			return
		}
		if parentPageID != pageID {
			writeError(w, http.StatusBadRequest, "bad_request", "parent comment belongs to a different page")
			return
		}
		if parentParentID.Valid {
			writeError(w, http.StatusBadRequest, "comment_reply_to_reply", "replies must target a root comment")
			return
		}
	} else {
		if !anchorTriplePopulated(req.AnchorPrefix, req.AnchorExact, req.AnchorSuffix) {
			writeError(w, http.StatusBadRequest, "comment_no_anchor", "root comments require anchor_prefix, anchor_exact, anchor_suffix")
			return
		}
		anchorPrefix = *req.AnchorPrefix
		anchorExact = *req.AnchorExact
		anchorSuffix = *req.AnchorSuffix
	}

	parentArg := any(nil)
	if isReply {
		parentArg = *req.ParentID
	}

	var id int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO comments
		  (page_id, parent_id, author_id, body,
		   anchor_prefix, anchor_exact, anchor_suffix,
		   resolved, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0, tela_now(), tela_now()) RETURNING id`,
		pageID, parentArg, u.ID, body, anchorPrefix, anchorExact, anchorSuffix).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create comment failed")
		return
	}
	c, err := selectCommentByIDTx(ctx, tx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch created comment failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"comment": c})
}

// PatchComment handles two mutually-exclusive operations on a comment:
//
//  1. {body: "..."} — author-only edit of the comment text.
//  2. {resolved: bool} — editor+ on the page's space toggles the resolved
//     flag. Only valid on root comments; flipping the same value twice
//     returns 409 comment_already_resolved.
//
// Sending both fields in one request returns 400 bad_request.
func (s *Server) PatchComment(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req commentPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not parse request body")
		return
	}
	switch {
	case req.Body != nil && req.Resolved != nil:
		writeError(w, http.StatusBadRequest, "bad_request", "body and resolved cannot be set in the same request")
		return
	case req.Body == nil && req.Resolved == nil:
		writeError(w, http.StatusBadRequest, "bad_request", "one of body, resolved must be provided")
		return
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	var (
		pageID    int64
		authorID  int64
		parentID  sql.NullInt64
		resolved  int
		deletedAt sql.NullString
	)
	err = tx.QueryRowContext(ctx,
		`SELECT page_id, author_id, parent_id, resolved, deleted_at FROM comments WHERE id = $1`, id).
		Scan(&pageID, &authorID, &parentID, &resolved, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "comment_not_found", "comment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup comment failed")
		return
	}
	if deletedAt.Valid {
		writeError(w, http.StatusNotFound, "comment_not_found", "comment not found")
		return
	}

	page, err := selectPageByIDTx(ctx, tx, pageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup parent page failed")
		return
	}
	if !enforceAPIKeySpaceScope(w, r, page.SpaceID) {
		return
	}
	role, err := spaceRoleTx(ctx, tx, u.ID, page.SpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup membership failed")
		return
	}
	if !canEdit(role) {
		writeError(w, http.StatusForbidden, "forbidden", "editor or owner role required")
		return
	}

	switch {
	case req.Body != nil:
		if authorID != u.ID {
			writeError(w, http.StatusForbidden, "forbidden", "only the author can edit a comment")
			return
		}
		body := strings.TrimSpace(*req.Body)
		if body == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "body is required")
			return
		}
		if len(body) > maxCommentBodyLen {
			writeError(w, http.StatusBadRequest, "bad_request", "body exceeds 10000 characters")
			return
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE comments SET body = $1, updated_at = tela_now() WHERE id = $2`,
			body, id); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "update comment failed")
			return
		}
	case req.Resolved != nil:
		if parentID.Valid {
			writeError(w, http.StatusBadRequest, "bad_request", "resolve can only be set on root comments")
			return
		}
		desired := 0
		if *req.Resolved {
			desired = 1
		}
		if resolved == desired {
			if desired == 1 {
				writeError(w, http.StatusConflict, "comment_already_resolved", "comment is already resolved")
			} else {
				writeError(w, http.StatusConflict, "comment_already_resolved", "comment is already open")
			}
			return
		}
		if desired == 1 {
			if _, err := tx.ExecContext(ctx, `
				UPDATE comments
				   SET resolved = 1, resolved_at = tela_now(), resolved_by = $1,
				       updated_at = tela_now()
				 WHERE id = $2`, u.ID, id); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "resolve comment failed")
				return
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				UPDATE comments
				   SET resolved = 0, resolved_at = NULL, resolved_by = NULL,
				       updated_at = tela_now()
				 WHERE id = $1`, id); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "reopen comment failed")
				return
			}
		}
	}

	c, err := selectCommentByIDTx(ctx, tx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch updated comment failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comment": c})
}

// DeleteComment soft-deletes a comment (sets deleted_at). The author may
// always delete their own; a space owner may delete any. Other editors of
// the space cannot delete comments authored by someone else.
func (s *Server) DeleteComment(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	var (
		pageID    int64
		authorID  int64
		deletedAt sql.NullString
	)
	err = tx.QueryRowContext(ctx,
		`SELECT page_id, author_id, deleted_at FROM comments WHERE id = $1`, id).
		Scan(&pageID, &authorID, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "comment_not_found", "comment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup comment failed")
		return
	}
	if deletedAt.Valid {
		writeError(w, http.StatusNotFound, "comment_not_found", "comment not found")
		return
	}

	page, err := selectPageByIDTx(ctx, tx, pageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup parent page failed")
		return
	}
	if !enforceAPIKeySpaceScope(w, r, page.SpaceID) {
		return
	}
	role, err := spaceRoleTx(ctx, tx, u.ID, page.SpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup membership failed")
		return
	}
	if !canEdit(role) {
		writeError(w, http.StatusForbidden, "forbidden", "editor or owner role required")
		return
	}
	// Author always allowed; otherwise only space owners.
	if authorID != u.ID && role != roleOwner {
		writeError(w, http.StatusForbidden, "forbidden", "only the author or a space owner can delete a comment")
		return
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE comments SET deleted_at = tela_now(), updated_at = tela_now() WHERE id = $1`,
		id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete comment failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// commentSelectColumns is the shared SELECT prefix used by every comment read
// path so scanCommentInto's column order is single-sourced.
const commentSelectColumns = `
	SELECT c.id, c.page_id, c.parent_id, c.author_id, author.username,
	       c.body, c.anchor_prefix, c.anchor_exact, c.anchor_suffix,
	       c.resolved, c.resolved_at, c.resolved_by, resolver.username,
	       c.created_at, c.updated_at`

func selectCommentByIDTx(ctx context.Context, tx *sql.Tx, id int64) (models.Comment, error) {
	row := tx.QueryRowContext(ctx, commentSelectColumns+`
		  FROM comments c
		  JOIN users author ON author.id = c.author_id
		  LEFT JOIN users resolver ON resolver.id = c.resolved_by
		 WHERE c.id = $1`, id)
	return scanCommentFromRow(row)
}

func scanCommentFromRow(row *sql.Row) (models.Comment, error) {
	return scanCommentInto(row)
}

func scanCommentFromRows(rows *sql.Rows) (models.Comment, error) {
	return scanCommentInto(rows)
}

func scanCommentInto(r rowScanner) (models.Comment, error) {
	var c models.Comment
	var (
		parentID     sql.NullInt64
		anchorPrefix sql.NullString
		anchorExact  sql.NullString
		anchorSuffix sql.NullString
		resolvedInt  int
		resolvedAt   sql.NullString
		resolvedBy   sql.NullInt64
		resolverName sql.NullString
	)
	if err := r.Scan(
		&c.ID, &c.PageID, &parentID, &c.AuthorID, &c.AuthorName,
		&c.Body, &anchorPrefix, &anchorExact, &anchorSuffix,
		&resolvedInt, &resolvedAt, &resolvedBy, &resolverName,
		&c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return c, err
	}
	if parentID.Valid {
		v := parentID.Int64
		c.ParentID = &v
	}
	if anchorPrefix.Valid {
		v := anchorPrefix.String
		c.AnchorPrefix = &v
	}
	if anchorExact.Valid {
		v := anchorExact.String
		c.AnchorExact = &v
	}
	if anchorSuffix.Valid {
		v := anchorSuffix.String
		c.AnchorSuffix = &v
	}
	c.Resolved = resolvedInt != 0
	if resolvedAt.Valid {
		v := resolvedAt.String
		c.ResolvedAt = &v
	}
	if resolvedBy.Valid {
		v := resolvedBy.Int64
		c.ResolvedBy = &v
	}
	if resolverName.Valid {
		v := resolverName.String
		c.ResolvedName = &v
	}
	return c, nil
}

// anchorTriplePopulated returns true when all three pointers are non-nil and
// the exact slice is non-empty. Empty exact would be a zero-length selection
// — the FE must guard, but the backend rejects it defensively here.
func anchorTriplePopulated(prefix, exact, suffix *string) bool {
	return prefix != nil && exact != nil && suffix != nil && *exact != ""
}
