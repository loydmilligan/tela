package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/zcag/tela/backend/internal/models"
)

const (
	pageRevisionsDefaultLimit = 50
	pageRevisionsMaxLimit     = 200
)

// pageRevisionListColumns is the SELECT used by the list endpoint — body
// is intentionally excluded so the per-row payload stays small.
const pageRevisionListColumns = `
	SELECT r.id, r.page_id, r.title, r.author_id, u.username,
	       r.source, r.byte_size, r.created_at`

// pageRevisionFullColumns is the SELECT used by the single-revision endpoint
// — adds the body column for the soft-draft / diff payload.
const pageRevisionFullColumns = `
	SELECT r.id, r.page_id, r.title, r.body, r.author_id, u.username,
	       r.source, r.byte_size, r.created_at`

// insertPageRevision writes a new page_revisions row for pageID. byte_size is
// derived from len(body); created_at is set by tela_now() so the
// wire format matches the rest of the API. authorID is nullable; pass nil when
// the writer's user record is unavailable. Called from the snapshot-on-save
// hook AFTER the pages UPDATE has committed, so a failure here cannot roll the
// user's save back.
func insertPageRevision(ctx context.Context, db *sql.DB, pageID int64, body, title string, authorID *int64, source string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO page_revisions
		  (page_id, body, title, author_id, source, byte_size, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, tela_now()) RETURNING id`,
		pageID, body, title, nullableInt64(authorID), source, int64(len(body))).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ListPageRevisions returns a paginated list of revisions for a page,
// ordered by id DESC (newest first). cursor=0 means "start from the latest";
// otherwise return rows with id < cursor. Editor+ on the page's space.
func (s *Server) ListPageRevisions(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusForbidden, "viewer_no_write", "editor or owner role required")
		return
	}

	q := r.URL.Query()
	cursor := int64(0)
	if raw := q.Get("cursor"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "cursor must be a non-negative integer")
			return
		}
		cursor = v
	}
	limit := int64(pageRevisionsDefaultLimit)
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be a positive integer")
			return
		}
		if v > pageRevisionsMaxLimit {
			v = pageRevisionsMaxLimit
		}
		limit = v
	}

	var rows *sql.Rows
	if cursor == 0 {
		rows, err = s.DB.QueryContext(ctx, pageRevisionListColumns+`
			  FROM page_revisions r
			  LEFT JOIN users u ON u.id = r.author_id
			 WHERE r.page_id = $1
			 ORDER BY r.id DESC
			 LIMIT $2`, pageID, limit)
	} else {
		rows, err = s.DB.QueryContext(ctx, pageRevisionListColumns+`
			  FROM page_revisions r
			  LEFT JOIN users u ON u.id = r.author_id
			 WHERE r.page_id = $1 AND r.id < $2
			 ORDER BY r.id DESC
			 LIMIT $3`, pageID, cursor, limit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list revisions failed")
		return
	}
	defer rows.Close()

	out := []models.PageRevision{}
	for rows.Next() {
		rev, err := scanPageRevisionList(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan revision row failed")
			return
		}
		out = append(out, rev)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate revisions failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": out})
}

// GetPageRevision returns a single revision's full body+title. Editor+ on the
// page's space required. Revision belonging to a different page than the
// {id} in the URL is treated as not-found (don't leak existence).
func (s *Server) GetPageRevision(w http.ResponseWriter, r *http.Request) {
	pageID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	revID, ok := parseIDParam(w, r, "rev_id")
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
		writeError(w, http.StatusForbidden, "viewer_no_write", "editor or owner role required")
		return
	}

	row := s.DB.QueryRowContext(ctx, pageRevisionFullColumns+`
		  FROM page_revisions r
		  LEFT JOIN users u ON u.id = r.author_id
		 WHERE r.id = $1 AND r.page_id = $2`, revID, pageID)
	rev, err := scanPageRevisionFull(row)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "revision_not_found", "revision not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch revision failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": rev})
}

func scanPageRevisionList(rows *sql.Rows) (models.PageRevision, error) {
	var rev models.PageRevision
	var (
		authorID   sql.NullInt64
		authorName sql.NullString
	)
	if err := rows.Scan(
		&rev.ID, &rev.PageID, &rev.Title, &authorID, &authorName,
		&rev.Source, &rev.ByteSize, &rev.CreatedAt,
	); err != nil {
		return rev, err
	}
	if authorID.Valid {
		v := authorID.Int64
		rev.AuthorID = &v
	}
	if authorName.Valid {
		v := authorName.String
		rev.AuthorUsername = &v
	}
	return rev, nil
}

func scanPageRevisionFull(row *sql.Row) (models.PageRevision, error) {
	var rev models.PageRevision
	var (
		authorID   sql.NullInt64
		authorName sql.NullString
	)
	if err := row.Scan(
		&rev.ID, &rev.PageID, &rev.Title, &rev.Body, &authorID, &authorName,
		&rev.Source, &rev.ByteSize, &rev.CreatedAt,
	); err != nil {
		return rev, err
	}
	if authorID.Valid {
		v := authorID.Int64
		rev.AuthorID = &v
	}
	if authorName.Valid {
		v := authorName.String
		rev.AuthorUsername = &v
	}
	return rev, nil
}
