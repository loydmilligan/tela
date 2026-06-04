package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/models"
)

const maxPageTitleLen = 500

type pageCreateRequest struct {
	SpaceID  int64  `json:"space_id"`
	ParentID *int64 `json:"parent_id"`
	Title    string `json:"title"`
	Body     string `json:"body"`
}

type pageUpdateRequest struct {
	Title *string `json:"title"`
	Body  *string `json:"body"`
}

// pageNode mirrors models.Page (promoted via embedding) plus a children slice
// so the tree endpoint can return a nested structure. Exposure is the resolved
// public-link visibility (see exposure.go) so the sidebar can render markers.
type pageNode struct {
	models.Page
	Exposure *pageExposure `json:"exposure"`
	Children []*pageNode   `json:"children"`
}

// pageWithExposure is the flat-list row enriched with resolved exposure, used
// by the non-tree ListPages branch so lazy-loaded children carry their state.
type pageWithExposure struct {
	models.Page
	Exposure *pageExposure `json:"exposure"`
}

// pageListItem is the flat cross-space row returned by ListAllPages — no
// body, no parent_id, no timestamps; just enough for the wikilink picker
// (id, space hint, title, breadcrumb).
type pageListItem struct {
	ID         int64    `json:"id"`
	SpaceID    int64    `json:"space_id"`
	SpaceName  string   `json:"space_name"`
	Title      string   `json:"title"`
	Breadcrumb []string `json:"breadcrumb"`
}

func (s *Server) ListPages(w http.ResponseWriter, r *http.Request) {
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

	if q.Get("tree") == "1" {
		tree, err := buildPageTree(r.Context(), s.DB, spaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "build page tree failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"pages": tree})
		return
	}

	parentIDStr := q.Get("parent_id")
	parentIDPresent := q.Has("parent_id")

	var rows *sql.Rows
	switch {
	case !parentIDPresent || parentIDStr == "" || parentIDStr == "null":
		rows, err = s.DB.QueryContext(r.Context(),
			`SELECT id, space_id, parent_id, title, body, position, created_at, updated_at
			 FROM pages WHERE space_id = $1 AND parent_id IS NULL
			 ORDER BY position ASC, id ASC`, spaceID)
	default:
		parentID, perr := strconv.ParseInt(parentIDStr, 10, 64)
		if perr != nil || parentID <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_query", "parent_id must be a positive integer or 'null'")
			return
		}
		rows, err = s.DB.QueryContext(r.Context(),
			`SELECT id, space_id, parent_id, title, body, position, created_at, updated_at
			 FROM pages WHERE space_id = $1 AND parent_id = $2
			 ORDER BY position ASC, id ASC`, spaceID, parentID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list pages failed")
		return
	}
	defer rows.Close()

	pages := []models.Page{}
	for rows.Next() {
		p, err := scanPageFromRows(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan page row failed")
			return
		}
		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate pages failed")
		return
	}

	exposures, err := resolveSpaceExposures(r.Context(), s.DB, spaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "resolve exposures failed")
		return
	}
	out := make([]pageWithExposure, 0, len(pages))
	for _, p := range pages {
		e := exposures[p.ID]
		out = append(out, pageWithExposure{Page: p, Exposure: &e})
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": out})
}

// ListAllPages returns every page in every space the caller is a member of
// as a flat list, ordered by space_name then title. Powers the cross-space
// `[[Page]]` picker. No pagination — single-user, <100 pages assumed (same
// bound as the orama tier-1 index).
func (s *Server) ListAllPages(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	// Bearer-mode with a space_id restriction narrows the cross-space listing
	// to just that one space. Without the filter the wikilink picker would
	// surface page ids the key isn't allowed to open (the actual GET would
	// then 403 — confusing UX, defends against accidental id leakage).
	var (
		rows *sql.Rows
		err  error
	)
	if k, isBearer := auth.APIKeyFromContext(r.Context()); isBearer && k.SpaceID != nil {
		rows, err = s.DB.QueryContext(r.Context(), `
			SELECT p.id, p.space_id, s.name, p.title
			  FROM pages p
			  JOIN spaces s ON s.id = p.space_id
			  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm ON sm.space_id = p.space_id
			 WHERE p.space_id = $2
			 ORDER BY s.name ASC, p.title ASC`, u.ID, *k.SpaceID)
	} else {
		rows, err = s.DB.QueryContext(r.Context(), `
			SELECT p.id, p.space_id, s.name, p.title
			  FROM pages p
			  JOIN spaces s ON s.id = p.space_id
			  JOIN (SELECT DISTINCT space_id FROM space_access WHERE user_id = $1) sm ON sm.space_id = p.space_id
			 ORDER BY s.name ASC, p.title ASC`, u.ID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list pages failed")
		return
	}
	defer rows.Close()

	type rowItem struct {
		ID, SpaceID      int64
		SpaceName, Title string
	}
	items := []rowItem{}
	for rows.Next() {
		var it rowItem
		if err := rows.Scan(&it.ID, &it.SpaceID, &it.SpaceName, &it.Title); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan page row failed")
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "iterate pages failed")
		return
	}

	out := make([]pageListItem, 0, len(items))
	for _, it := range items {
		bc, err := pageBreadcrumb(r.Context(), s.DB, it.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "build breadcrumb failed")
			return
		}
		out = append(out, pageListItem{
			ID:         it.ID,
			SpaceID:    it.SpaceID,
			SpaceName:  it.SpaceName,
			Title:      it.Title,
			Breadcrumb: bc,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"pages": out})
}

func (s *Server) CreatePage(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req pageCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "invalid_title", "title is required")
		return
	}
	if len(title) > maxPageTitleLen {
		writeError(w, http.StatusBadRequest, "invalid_title", "title exceeds 500 characters")
		return
	}
	if req.SpaceID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_space_id", "space_id is required")
		return
	}
	if req.ParentID != nil && *req.ParentID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_parent_id", "parent_id must be a positive integer or null")
		return
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	if !enforceAPIKeySpaceScope(w, r, req.SpaceID) {
		return
	}
	role, err := spaceRoleTx(ctx, tx, u.ID, req.SpaceID)
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

	if err := verifySpaceExistsTx(ctx, tx, req.SpaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "space_not_found", "space does not exist")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "lookup space failed")
		return
	}

	if req.ParentID != nil {
		var parentSpaceID int64
		err := tx.QueryRowContext(ctx,
			`SELECT space_id FROM pages WHERE id = $1`, *req.ParentID).Scan(&parentSpaceID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "parent_not_found", "parent page does not exist")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "lookup parent failed")
			return
		}
		if parentSpaceID != req.SpaceID {
			writeError(w, http.StatusBadRequest, "parent_space_mismatch", "parent page is in a different space")
			return
		}
	}

	var maxPos sql.NullInt64
	if req.ParentID == nil {
		err = tx.QueryRowContext(ctx,
			`SELECT MAX(position) FROM pages WHERE space_id = $1 AND parent_id IS NULL`, req.SpaceID).Scan(&maxPos)
	} else {
		err = tx.QueryRowContext(ctx,
			`SELECT MAX(position) FROM pages WHERE space_id = $1 AND parent_id = $2`, req.SpaceID, *req.ParentID).Scan(&maxPos)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "compute position failed")
		return
	}
	var position int64
	if maxPos.Valid {
		position = maxPos.Int64 + 1
	}

	var id int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO pages(space_id, parent_id, title, body, position) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		req.SpaceID, nullableInt64(req.ParentID), title, req.Body, position).Scan(&id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create page failed")
		return
	}
	if err := syncPageLinks(ctx, tx, id, req.Body); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "sync page_links failed")
		return
	}
	page, err := selectPageByIDTx(ctx, tx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch created page failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"page": page})
}

func (s *Server) GetPage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	p, err := selectPageByID(r.Context(), s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		// Collapse missing-page to the same 403 a non-member would see, so
		// callers cannot enumerate page ids across spaces they're not in.
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch page failed")
		return
	}
	if _, ok := s.requireMembership(w, r, p.SpaceID); !ok {
		return
	}
	exp, err := resolvePageExposure(r.Context(), s.DB, p.ID, p.SpaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "resolve exposure failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": p, "exposure": exp})
}

func (s *Server) UpdatePage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req pageUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}
	if req.Title == nil && req.Body == nil {
		writeError(w, http.StatusBadRequest, "no_fields", "at least one of title, body must be provided")
		return
	}

	sets := make([]string, 0, 3)
	args := make([]any, 0, 3)

	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "invalid_title", "title cannot be empty")
			return
		}
		if len(title) > maxPageTitleLen {
			writeError(w, http.StatusBadRequest, "invalid_title", "title exceeds 500 characters")
			return
		}
		args = append(args, title)
		sets = append(sets, "title = $"+strconv.Itoa(len(args)))
	}
	if req.Body != nil {
		args = append(args, *req.Body)
		sets = append(sets, "body = $"+strconv.Itoa(len(args)))
	}
	sets = append(sets, "updated_at = tela_now()")
	args = append(args, id)

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	existing, err := selectPageByIDTx(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		// Collapse missing-page to 403 so non-members cannot tell
		// "exists in another space" from "truly gone".
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
		return
	}
	if !enforceAPIKeySpaceScope(w, r, existing.SpaceID) {
		return
	}
	role, err := spaceRoleTx(ctx, tx, u.ID, existing.SpaceID)
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

	stmt := "UPDATE pages SET " + strings.Join(sets, ", ") + " WHERE id = $" + strconv.Itoa(len(args))
	res, err := tx.ExecContext(ctx, stmt, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "update page failed")
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "update page: rows affected failed")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	if req.Body != nil {
		if err := syncPageLinks(ctx, tx, id, *req.Body); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "sync page_links failed")
			return
		}
	}
	p, err := selectPageByIDTx(ctx, tx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch updated page failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	// M9.0 snapshot-on-save: every persisted PATCH that actually changes body
	// or title writes a page_revisions row. Runs AFTER commit so a failure
	// here cannot roll the user's save back — we log and proceed.
	if p.Body != existing.Body || p.Title != existing.Title {
		authorID := u.ID
		if _, err := insertPageRevision(ctx, s.DB, id, p.Body, p.Title, &authorID, "manual"); err != nil {
			log.Printf("page %d snapshot revision failed: %v", id, err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": p})
}

func (s *Server) DeletePage(w http.ResponseWriter, r *http.Request) {
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

	existing, err := selectPageByIDTx(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		// Collapse missing-page to 403 so non-members cannot tell
		// "exists in another space" from "truly gone".
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
		return
	}
	if !enforceAPIKeySpaceScope(w, r, existing.SpaceID) {
		return
	}
	role, err := spaceRoleTx(ctx, tx, u.ID, existing.SpaceID)
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

	// Cache the live title onto any incoming page_links rows so backlinks
	// from other pages still render with a usable label after deletion.
	if _, err := tx.ExecContext(ctx,
		`UPDATE page_links
		   SET last_known_title = COALESCE((SELECT title FROM pages WHERE id = $1), last_known_title)
		 WHERE target_id = $2`, id, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "cache page_links titles failed")
		return
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete page failed")
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete page: rows affected failed")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	}

	// Explicitly clear outgoing rows for the deleted source — no FK / no
	// triggers; nothing else would remove them.
	if _, err := tx.ExecContext(ctx, `DELETE FROM page_links WHERE source_id = $1`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete page_links source rows failed")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) MovePage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}

	var rawMap map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawMap); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "could not parse request body")
		return
	}

	spaceIDSet := false
	var newSpaceID int64
	if v, ok := rawMap["space_id"]; ok {
		spaceIDSet = true
		if err := json.Unmarshal(v, &newSpaceID); err != nil || newSpaceID <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_space_id", "space_id must be a positive integer")
			return
		}
	}
	parentIDSet := false
	parentIDIsNull := false
	var newParentID int64
	if v, ok := rawMap["parent_id"]; ok {
		parentIDSet = true
		if string(v) == "null" {
			parentIDIsNull = true
		} else {
			if err := json.Unmarshal(v, &newParentID); err != nil || newParentID <= 0 {
				writeError(w, http.StatusBadRequest, "invalid_parent_id", "parent_id must be a positive integer or null")
				return
			}
		}
	}
	positionSet := false
	var newPosition int64
	if v, ok := rawMap["position"]; ok {
		positionSet = true
		if err := json.Unmarshal(v, &newPosition); err != nil || newPosition < 0 {
			writeError(w, http.StatusBadRequest, "invalid_position", "position must be a non-negative integer")
			return
		}
	}
	if !spaceIDSet && !parentIDSet && !positionSet {
		writeError(w, http.StatusBadRequest, "no_fields", "at least one of space_id, parent_id, position must be provided")
		return
	}

	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "begin tx failed")
		return
	}
	defer tx.Rollback()

	page, err := selectPageByIDTx(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		// Collapse missing-page to 403 so non-members cannot tell
		// "exists in another space" from "truly gone".
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

	sourceRole, err := spaceRoleTx(ctx, tx, u.ID, page.SpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusForbidden, "forbidden", "not a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup membership failed")
		return
	}
	if !canEdit(sourceRole) {
		writeError(w, http.StatusForbidden, "forbidden", "editor or owner role required")
		return
	}

	targetSpaceID := page.SpaceID
	if spaceIDSet {
		targetSpaceID = newSpaceID
		if err := verifySpaceExistsTx(ctx, tx, targetSpaceID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusBadRequest, "space_not_found", "target space does not exist")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "lookup target space failed")
			return
		}
		if targetSpaceID != page.SpaceID {
			if !enforceAPIKeySpaceScope(w, r, targetSpaceID) {
				return
			}
			targetRole, err := spaceRoleTx(ctx, tx, u.ID, targetSpaceID)
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusForbidden, "forbidden", "not a member of target space")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "lookup target membership failed")
				return
			}
			if !canEdit(targetRole) {
				writeError(w, http.StatusForbidden, "forbidden", "editor or owner role required in target space")
				return
			}
		}
	}

	var targetParentID *int64
	if parentIDSet {
		if !parentIDIsNull {
			parent := newParentID
			targetParentID = &parent
		}
	} else {
		targetParentID = page.ParentID
	}

	if targetParentID != nil {
		var parentSpaceID int64
		err := tx.QueryRowContext(ctx,
			`SELECT space_id FROM pages WHERE id = $1`, *targetParentID).Scan(&parentSpaceID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "parent_not_found", "parent page does not exist")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "lookup parent failed")
			return
		}
		if parentSpaceID != targetSpaceID {
			writeError(w, http.StatusBadRequest, "parent_space_mismatch", "parent page is in a different space")
			return
		}
		if cyclic, err := wouldCreateCycle(ctx, tx, id, *targetParentID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "cycle check failed")
			return
		} else if cyclic {
			writeError(w, http.StatusBadRequest, "cycle", "move would create a cycle")
			return
		}
	}

	sameGroup := page.SpaceID == targetSpaceID && parentIDPtrEqual(page.ParentID, targetParentID)

	newSiblingIDs, err := siblingIDsExcluding(ctx, tx, targetSpaceID, targetParentID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list new siblings failed")
		return
	}

	insertAt := int64(len(newSiblingIDs))
	if positionSet && newPosition < insertAt {
		insertAt = newPosition
	}

	finalList := make([]int64, 0, len(newSiblingIDs)+1)
	finalList = append(finalList, newSiblingIDs[:insertAt]...)
	finalList = append(finalList, id)
	finalList = append(finalList, newSiblingIDs[insertAt:]...)

	if _, err := tx.ExecContext(ctx,
		`UPDATE pages SET space_id = $1, parent_id = $2, updated_at = tela_now() WHERE id = $3`,
		targetSpaceID, nullableInt64(targetParentID), id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "move page failed")
		return
	}

	if page.SpaceID != targetSpaceID {
		if err := updateDescendantsSpaceID(ctx, tx, id, targetSpaceID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "propagate space_id to descendants failed")
			return
		}
	}

	for i, sid := range finalList {
		if _, err := tx.ExecContext(ctx,
			`UPDATE pages SET position = $1 WHERE id = $2`, int64(i), sid); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "renumber new siblings failed")
			return
		}
	}

	if !sameGroup {
		oldSiblingIDs, err := siblingIDsExcluding(ctx, tx, page.SpaceID, page.ParentID, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "list old siblings failed")
			return
		}
		for i, sid := range oldSiblingIDs {
			if _, err := tx.ExecContext(ctx,
				`UPDATE pages SET position = $1 WHERE id = $2`, int64(i), sid); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "renumber old siblings failed")
				return
			}
		}
	}

	moved, err := selectPageByIDTx(ctx, tx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch moved page failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "commit failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": moved})
}

func buildPageTree(ctx context.Context, db *sql.DB, spaceID int64) ([]*pageNode, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, space_id, parent_id, title, body, position, created_at, updated_at
		 FROM pages WHERE space_id = $1
		 ORDER BY position ASC, id ASC`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodes := map[int64]*pageNode{}
	all := []*pageNode{}
	for rows.Next() {
		p, err := scanPageFromRows(rows)
		if err != nil {
			return nil, err
		}
		n := &pageNode{Page: p, Children: []*pageNode{}}
		nodes[p.ID] = n
		all = append(all, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Resolve exposure off the already-loaded nodes (build the parent map in
	// memory; only the active-share lookup hits the DB).
	parentMap := make(map[int64]*int64, len(all))
	for _, n := range all {
		parentMap[n.ID] = n.ParentID
	}
	shares, err := loadActiveShareFacts(ctx, db, spaceID)
	if err != nil {
		return nil, err
	}
	exposures := resolveExposures(parentMap, shares)
	for _, n := range all {
		e := exposures[n.ID]
		n.Exposure = &e
	}

	roots := []*pageNode{}
	for _, n := range all {
		if n.ParentID == nil {
			roots = append(roots, n)
			continue
		}
		if parent, ok := nodes[*n.ParentID]; ok {
			parent.Children = append(parent.Children, n)
		}
	}
	return roots, nil
}

func selectPageByID(ctx context.Context, db *sql.DB, id int64) (models.Page, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, space_id, parent_id, title, body, position, created_at, updated_at
		 FROM pages WHERE id = $1`, id)
	return scanPageFromRow(row)
}

func selectPageByIDTx(ctx context.Context, tx *sql.Tx, id int64) (models.Page, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT id, space_id, parent_id, title, body, position, created_at, updated_at
		 FROM pages WHERE id = $1`, id)
	return scanPageFromRow(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPageFromRow(row *sql.Row) (models.Page, error) {
	return scanPageInto(row)
}

func scanPageFromRows(rows *sql.Rows) (models.Page, error) {
	return scanPageInto(rows)
}

func scanPageInto(r rowScanner) (models.Page, error) {
	var p models.Page
	var parentID sql.NullInt64
	if err := r.Scan(&p.ID, &p.SpaceID, &parentID, &p.Title, &p.Body, &p.Position, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return p, err
	}
	if parentID.Valid {
		v := parentID.Int64
		p.ParentID = &v
	}
	return p, nil
}

func verifySpaceExists(ctx context.Context, db *sql.DB, id int64) error {
	var x int
	return db.QueryRowContext(ctx, `SELECT 1 FROM spaces WHERE id = $1`, id).Scan(&x)
}

func verifySpaceExistsTx(ctx context.Context, tx *sql.Tx, id int64) error {
	var x int
	return tx.QueryRowContext(ctx, `SELECT 1 FROM spaces WHERE id = $1`, id).Scan(&x)
}

func siblingIDsExcluding(ctx context.Context, tx *sql.Tx, spaceID int64, parentID *int64, excludeID int64) ([]int64, error) {
	var rows *sql.Rows
	var err error
	if parentID == nil {
		rows, err = tx.QueryContext(ctx,
			`SELECT id FROM pages WHERE space_id = $1 AND parent_id IS NULL AND id != $2
			 ORDER BY position ASC, id ASC`, spaceID, excludeID)
	} else {
		rows, err = tx.QueryContext(ctx,
			`SELECT id FROM pages WHERE space_id = $1 AND parent_id = $2 AND id != $3
			 ORDER BY position ASC, id ASC`, spaceID, *parentID, excludeID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []int64{}
	for rows.Next() {
		var sid int64
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		ids = append(ids, sid)
	}
	return ids, rows.Err()
}

// wouldCreateCycle returns true if making movingID a child of newParentID
// would put movingID in its own ancestor chain.
func wouldCreateCycle(ctx context.Context, tx *sql.Tx, movingID, newParentID int64) (bool, error) {
	cursor := newParentID
	for {
		if cursor == movingID {
			return true, nil
		}
		var pid sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT parent_id FROM pages WHERE id = $1`, cursor).Scan(&pid)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !pid.Valid {
			return false, nil
		}
		cursor = pid.Int64
	}
}

// updateDescendantsSpaceID updates space_id for all descendants of rootID
// (rootID itself is NOT updated — caller is expected to have already done so).
func updateDescendantsSpaceID(ctx context.Context, tx *sql.Tx, rootID, newSpaceID int64) error {
	frontier := []int64{rootID}
	for len(frontier) > 0 {
		placeholders := make([]string, len(frontier))
		args := make([]any, len(frontier))
		for i, fid := range frontier {
			placeholders[i] = "$" + strconv.Itoa(i+1)
			args[i] = fid
		}
		q := fmt.Sprintf(`SELECT id FROM pages WHERE parent_id IN (%s)`, strings.Join(placeholders, ","))
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		next := []int64{}
		for rows.Next() {
			var cid int64
			if err := rows.Scan(&cid); err != nil {
				rows.Close()
				return err
			}
			next = append(next, cid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(next) == 0 {
			return nil
		}
		updPlaceholders := make([]string, len(next))
		updArgs := make([]any, 0, len(next)+1)
		updArgs = append(updArgs, newSpaceID)
		for i, nid := range next {
			updPlaceholders[i] = "$" + strconv.Itoa(i+2)
			updArgs = append(updArgs, nid)
		}
		upd := fmt.Sprintf(`UPDATE pages SET space_id = $1 WHERE id IN (%s)`, strings.Join(updPlaceholders, ","))
		if _, err := tx.ExecContext(ctx, upd, updArgs...); err != nil {
			return err
		}
		frontier = next
	}
	return nil
}

func parentIDPtrEqual(a, b *int64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// wikiLinkRE matches the canonical wikilink URL form serialised by the
// Milkdown mention node. The URL is the source of truth — we don't parse
// the surrounding node syntax.
var wikiLinkRE = regexp.MustCompile(`tela://page/([0-9]+)`)

// parseWikiLinks extracts unique target page ids referenced by body via
// `tela://page/{N}` URLs. Order of returned ids is not guaranteed.
func parseWikiLinks(body string) []int64 {
	matches := wikiLinkRE.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(matches))
	ids := make([]int64, 0, len(matches))
	for _, m := range matches {
		n, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil || n <= 0 {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		ids = append(ids, n)
	}
	return ids
}

// syncPageLinks rebuilds the outgoing page_links rows for sourceID from
// body: deletes existing rows, then inserts one row per unique wikilink
// target. last_known_title is the live target title, or an empty string
// when the target does not exist — that's how a freshly broken link is
// recorded.
func syncPageLinks(ctx context.Context, tx *sql.Tx, sourceID int64, body string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM page_links WHERE source_id = $1`, sourceID); err != nil {
		return fmt.Errorf("delete outgoing page_links: %w", err)
	}
	targets := parseWikiLinks(body)
	if len(targets) == 0 {
		return nil
	}
	placeholders := make([]string, 0, len(targets))
	args := make([]any, 0, len(targets)*3)
	for _, tid := range targets {
		if tid == sourceID {
			continue
		}
		var title sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT title FROM pages WHERE id = $1`, tid).Scan(&title)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("lookup target title: %w", err)
		}
		n := len(args)
		placeholders = append(placeholders, fmt.Sprintf("($%d, $%d, $%d)", n+1, n+2, n+3))
		args = append(args, sourceID, tid, title.String)
	}
	if len(placeholders) == 0 {
		return nil
	}
	stmt := `INSERT INTO page_links(source_id, target_id, last_known_title) VALUES ` + strings.Join(placeholders, ", ")
	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("insert page_links: %w", err)
	}
	return nil
}
