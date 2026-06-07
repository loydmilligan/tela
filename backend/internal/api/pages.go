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
	"github.com/zcag/tela/backend/internal/pagemd"
)

const maxPageTitleLen = 500

type pageCreateRequest struct {
	SpaceID  int64          `json:"space_id"`
	ParentID *int64         `json:"parent_id"`
	Title    string         `json:"title"`
	Body     string         `json:"body"`
	Props    map[string]any `json:"props"`
}

// pageUpdateRequest patches a page. A nil field is left unchanged; a non-nil
// Props (including an explicit {}) replaces the whole bag (Replace/PUT semantics
// — see docs/page-properties.md "Update semantics").
type pageUpdateRequest struct {
	Title *string        `json:"title"`
	Body  *string        `json:"body"`
	Props map[string]any `json:"props"`
}

// propsJSON marshals a props bag to a JSON string for binding into a JSONB
// column (with a ::jsonb cast at the call site). Empty/nil → "{}".
func propsJSON(props map[string]any) string {
	if len(props) == 0 {
		return "{}"
	}
	b, err := json.Marshal(props)
	if err != nil {
		return "{}"
	}
	return string(b)
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

	var parentID *int64
	if parentIDPresent && parentIDStr != "" && parentIDStr != "null" {
		pid, perr := strconv.ParseInt(parentIDStr, 10, 64)
		if perr != nil || pid <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_query", "parent_id must be a positive integer or 'null'")
			return
		}
		parentID = &pid
	}

	out, err := listPagesFlat(r.Context(), s.DB, spaceID, parentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list pages failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": out})
}

// listPagesFlat returns the direct children of parentID in spaceID (roots when
// parentID is nil), each enriched with resolved exposure. Auth-free shared core
// behind the flat branch of GET /api/pages and the MCP list_pages tool — the
// caller must do space-exists + membership gating first (listPagesCore does for
// the MCP path; ListPages does inline for both its tree and flat branches).
func listPagesFlat(ctx context.Context, db *sql.DB, spaceID int64, parentID *int64) ([]pageWithExposure, error) {
	var rows *sql.Rows
	var err error
	if parentID == nil {
		rows, err = db.QueryContext(ctx,
			`SELECT id, space_id, parent_id, title, body, position, props, created_at, updated_at
			 FROM pages WHERE space_id = $1 AND parent_id IS NULL
			 ORDER BY position ASC, id ASC`, spaceID)
	} else {
		rows, err = db.QueryContext(ctx,
			`SELECT id, space_id, parent_id, title, body, position, props, created_at, updated_at
			 FROM pages WHERE space_id = $1 AND parent_id = $2
			 ORDER BY position ASC, id ASC`, spaceID, *parentID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pages := []models.Page{}
	for rows.Next() {
		p, err := scanPageFromRows(rows)
		if err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	exposures, err := resolveSpaceExposures(ctx, db, spaceID)
	if err != nil {
		return nil, err
	}
	out := make([]pageWithExposure, 0, len(pages))
	for _, p := range pages {
		e := exposures[p.ID]
		out = append(out, pageWithExposure{Page: p, Exposure: &e})
	}
	return out, nil
}

// listPagesCore is the transport-agnostic core behind the MCP list_pages tool:
// verify the space exists, gate on membership, then return the flat child list.
// Mirrors the flat branch of ListPages with the same checks in the same order.
func (s *Server) listPagesCore(ctx context.Context, u *auth.User, k *auth.APIKey, spaceID int64, parentID *int64) ([]pageWithExposure, *apiErr) {
	if err := verifySpaceExists(ctx, s.DB, spaceID); errors.Is(err, sql.ErrNoRows) {
		return nil, &apiErr{http.StatusNotFound, "space_not_found", "space not found"}
	} else if err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "lookup space failed"}
	}
	if _, ae := s.membershipCore(ctx, u, k, spaceID); ae != nil {
		return nil, ae
	}
	out, err := listPagesFlat(ctx, s.DB, spaceID, parentID)
	if err != nil {
		return nil, &apiErr{http.StatusInternalServerError, "internal", "list pages failed"}
	}
	return out, nil
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
	k, _ := auth.APIKeyFromContext(r.Context())
	page, ae := s.createPageCore(r.Context(), u, k, req)
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"page": page})
}

// createPageCore is the transport-agnostic core behind POST /api/pages and the
// MCP create_page tool: validate, gate on editor+ membership (bearer space-scope
// first), then insert the page and sync its outgoing wikilinks in one tx.
// leadingTitleH1RE matches a leading ATX H1 at the very top of a body (only
// whitespace may precede it), capturing the heading text.
var leadingTitleH1RE = regexp.MustCompile(`\A\s*#[ \t]+([^\r\n]+?)[ \t]*(\r?\n|\z)`)

// stripLeadingTitleH1 drops a leading `# Heading` from body when its text equals
// title (case-insensitive). Title and body are separate columns, but MCP/agent
// clients habitually open the markdown with `# Same Title`, which then renders
// twice — once as the page title, once as a body heading. A leading H1 that
// differs from the title is left untouched.
func stripLeadingTitleH1(body, title string) string {
	m := leadingTitleH1RE.FindStringSubmatchIndex(body)
	if m == nil {
		return body
	}
	heading := strings.TrimSpace(body[m[2]:m[3]])
	if !strings.EqualFold(heading, strings.TrimSpace(title)) {
		return body
	}
	return strings.TrimLeft(body[m[1]:], "\r\n")
}

func (s *Server) createPageCore(ctx context.Context, u *auth.User, k *auth.APIKey, req pageCreateRequest) (models.Page, *apiErr) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return models.Page{}, &apiErr{http.StatusBadRequest, "invalid_title", "title is required"}
	}
	// Body invariant: frontmatter never lives in pages.body, at any ingress.
	// Strip any leading frontmatter out of the body and absorb it into props.
	// Precedence: an explicit props field wins over frontmatter found in body.
	body, _, bodyProps := pagemd.Decode(req.Body)
	body = stripLeadingTitleH1(body, title)
	props := pagemd.FilterReserved(req.Props)
	if props == nil {
		props = bodyProps
	}
	if props == nil {
		props = map[string]any{}
	}
	if len(title) > maxPageTitleLen {
		return models.Page{}, &apiErr{http.StatusBadRequest, "invalid_title", "title exceeds 500 characters"}
	}
	if req.SpaceID <= 0 {
		return models.Page{}, &apiErr{http.StatusBadRequest, "invalid_space_id", "space_id is required"}
	}
	if req.ParentID != nil && *req.ParentID <= 0 {
		return models.Page{}, &apiErr{http.StatusBadRequest, "invalid_parent_id", "parent_id must be a positive integer or null"}
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "begin tx failed"}
	}
	defer tx.Rollback()

	if ae := apiKeySpaceScopeErr(k, req.SpaceID); ae != nil {
		return models.Page{}, ae
	}
	role, err := spaceRoleTx(ctx, tx, u.ID, req.SpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Page{}, &apiErr{http.StatusForbidden, "forbidden", "not a member"}
	}
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "lookup membership failed"}
	}
	if !canEdit(role) {
		return models.Page{}, &apiErr{http.StatusForbidden, "forbidden", "editor or owner role required"}
	}

	if err := verifySpaceExistsTx(ctx, tx, req.SpaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Page{}, &apiErr{http.StatusBadRequest, "space_not_found", "space does not exist"}
		}
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "lookup space failed"}
	}

	if req.ParentID != nil {
		var parentSpaceID int64
		err := tx.QueryRowContext(ctx,
			`SELECT space_id FROM pages WHERE id = $1`, *req.ParentID).Scan(&parentSpaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return models.Page{}, &apiErr{http.StatusBadRequest, "parent_not_found", "parent page does not exist"}
		}
		if err != nil {
			return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "lookup parent failed"}
		}
		if parentSpaceID != req.SpaceID {
			return models.Page{}, &apiErr{http.StatusBadRequest, "parent_space_mismatch", "parent page is in a different space"}
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
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "compute position failed"}
	}
	var position int64
	if maxPos.Valid {
		position = maxPos.Int64 + 1
	}

	var id int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO pages(space_id, parent_id, title, body, position, props) VALUES ($1, $2, $3, $4, $5, $6::jsonb) RETURNING id`,
		req.SpaceID, nullableInt64(req.ParentID), title, body, position, propsJSON(props)).Scan(&id); err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "create page failed"}
	}
	if err := syncPageLinks(ctx, tx, id, body); err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "sync page_links failed"}
	}
	page, err := selectPageByIDTx(ctx, tx, id)
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "fetch created page failed"}
	}
	if err := tx.Commit(); err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "commit failed"}
	}
	// Index the new page's content (debounced, async; no-op when RAG is off).
	// Lives in the core so both POST /api/pages and the MCP create_page tool
	// enqueue a reindex.
	s.rag.QueueReindex(id)
	return page, nil
}

func (s *Server) GetPage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	k, _ := auth.APIKeyFromContext(r.Context())
	p, ae := s.getPageCore(r.Context(), u, k, id)
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	exp, err := resolvePageExposure(r.Context(), s.DB, p.ID, p.SpaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "resolve exposure failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": p, "exposure": exp})
}

// getPageCore is the transport-agnostic core behind GET /api/pages/{id} and the
// MCP get_page tool: fetch the page and gate on space membership. Missing page
// collapses to the same 403 a non-member sees so ids can't be enumerated across
// spaces. The REST route additionally resolves exposure (the MCP tool doesn't
// need it).
func (s *Server) getPageCore(ctx context.Context, u *auth.User, k *auth.APIKey, id int64) (models.Page, *apiErr) {
	p, err := selectPageByID(ctx, s.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Page{}, &apiErr{http.StatusForbidden, "forbidden", "not a member"}
	}
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "fetch page failed"}
	}
	if _, ae := s.membershipCore(ctx, u, k, p.SpaceID); ae != nil {
		return models.Page{}, ae
	}
	return p, nil
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
	k, _ := auth.APIKeyFromContext(r.Context())
	// agentWrite=false: a REST save is the editor's own collab-synced write (or a
	// generic client); it must NOT drop the Yjs overlay it is in sync with.
	p, ae := s.updatePageCore(r.Context(), u, k, id, req, false)
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": p})
}

// updatePageCore is the transport-agnostic core behind PATCH /api/pages/{id} and
// the MCP update_page tool: patch title and/or body under editor+ membership,
// re-sync wikilinks when the body changes, and snapshot a revision after commit.
func (s *Server) updatePageCore(ctx context.Context, u *auth.User, k *auth.APIKey, id int64, req pageUpdateRequest, agentWrite bool) (models.Page, *apiErr) {
	if req.Title == nil && req.Body == nil && req.Props == nil {
		return models.Page{}, &apiErr{http.StatusBadRequest, "no_fields", "at least one of title, body, props must be provided"}
	}

	sets := make([]string, 0, 4)
	args := make([]any, 0, 4)

	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			return models.Page{}, &apiErr{http.StatusBadRequest, "invalid_title", "title cannot be empty"}
		}
		if len(title) > maxPageTitleLen {
			return models.Page{}, &apiErr{http.StatusBadRequest, "invalid_title", "title exceeds 500 characters"}
		}
		args = append(args, title)
		sets = append(sets, "title = $"+strconv.Itoa(len(args)))
	}
	// Body invariant: strip any leading frontmatter out of the incoming body and
	// absorb it into props (bodyStripped is what gets stored + link-synced).
	var bodyStripped string
	var bodyProps map[string]any
	if req.Body != nil {
		bodyStripped, _, bodyProps = pagemd.Decode(*req.Body)
		args = append(args, bodyStripped)
		sets = append(sets, "body = $"+strconv.Itoa(len(args)))
	}
	// Props: Replace semantics. An explicit props field wins over frontmatter
	// found in body; a nil field leaves the bag unchanged.
	if req.Props != nil {
		args = append(args, propsJSON(pagemd.FilterReserved(req.Props)))
		sets = append(sets, "props = $"+strconv.Itoa(len(args))+"::jsonb")
	} else if bodyProps != nil {
		args = append(args, propsJSON(bodyProps))
		sets = append(sets, "props = $"+strconv.Itoa(len(args))+"::jsonb")
	}
	sets = append(sets, "updated_at = tela_now()")
	args = append(args, id)

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "begin tx failed"}
	}
	defer tx.Rollback()

	existing, err := selectPageByIDTx(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Page{}, &apiErr{http.StatusForbidden, "forbidden", "not a member"}
	}
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "lookup page failed"}
	}
	if ae := apiKeySpaceScopeErr(k, existing.SpaceID); ae != nil {
		return models.Page{}, ae
	}
	role, err := spaceRoleTx(ctx, tx, u.ID, existing.SpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Page{}, &apiErr{http.StatusForbidden, "forbidden", "not a member"}
	}
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "lookup membership failed"}
	}
	if !canEdit(role) {
		return models.Page{}, &apiErr{http.StatusForbidden, "forbidden", "editor or owner role required"}
	}

	stmt := "UPDATE pages SET " + strings.Join(sets, ", ") + " WHERE id = $" + strconv.Itoa(len(args))
	res, err := tx.ExecContext(ctx, stmt, args...)
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "update page failed"}
	}
	n, err := res.RowsAffected()
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "update page: rows affected failed"}
	}
	if n == 0 {
		return models.Page{}, &apiErr{http.StatusNotFound, "not_found", "page not found"}
	}
	if req.Body != nil {
		if err := syncPageLinks(ctx, tx, id, bodyStripped); err != nil {
			return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "sync page_links failed"}
		}
	}
	p, err := selectPageByIDTx(ctx, tx, id)
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "fetch updated page failed"}
	}
	if err := tx.Commit(); err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "commit failed"}
	}
	// M9.0 snapshot-on-save: every persisted PATCH that actually changes body
	// or title writes a page_revisions row. Runs AFTER commit so a failure
	// here cannot roll the user's save back — we log and proceed.
	if p.Body != existing.Body || p.Title != existing.Title {
		authorID := u.ID
		if _, err := insertPageRevision(ctx, s.DB, id, p.Body, p.Title, p.Props, &authorID, "manual"); err != nil {
			log.Printf("page %d snapshot revision failed: %v", id, err)
		}
		// Title is folded into each chunk's embed text and body is the source,
		// so reindex on either change (debounced, async; no-op when RAG is off).
		s.rag.QueueReindex(id)
	}
	// When an agent rewrites the body out-of-band (MCP update_page), drop the
	// Yjs collab overlay so live + next editors re-seed from the new body instead
	// of masking it with stale CRDT state (which would also clobber the agent's
	// write on the next human save). DB-wins, per the agent-backend sync design.
	if agentWrite && req.Body != nil && p.Body != existing.Body {
		if err := s.rooms.resetPage(ctx, s.DB, id); err != nil {
			log.Printf("page %d collab overlay reset failed: %v", id, err)
		}
	}
	return p, nil
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
	k, _ := auth.APIKeyFromContext(r.Context())
	if ae := s.deletePageCore(r.Context(), u, k, id); ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deletePageCore is the transport-agnostic core behind DELETE /api/pages/{id}
// and the MCP delete_page tool: editor+ gated; caches the live title onto
// incoming backlinks before deleting, and clears the page's outgoing links.
func (s *Server) deletePageCore(ctx context.Context, u *auth.User, k *auth.APIKey, id int64) *apiErr {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "begin tx failed"}
	}
	defer tx.Rollback()

	existing, err := selectPageByIDTx(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return &apiErr{http.StatusForbidden, "forbidden", "not a member"}
	}
	if err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "lookup page failed"}
	}
	if ae := apiKeySpaceScopeErr(k, existing.SpaceID); ae != nil {
		return ae
	}
	role, err := spaceRoleTx(ctx, tx, u.ID, existing.SpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return &apiErr{http.StatusForbidden, "forbidden", "not a member"}
	}
	if err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "lookup membership failed"}
	}
	if !canEdit(role) {
		return &apiErr{http.StatusForbidden, "forbidden", "editor or owner role required"}
	}

	// Cache the live title onto any incoming page_links rows so backlinks
	// from other pages still render with a usable label after deletion.
	if _, err := tx.ExecContext(ctx,
		`UPDATE page_links
		   SET last_known_title = COALESCE((SELECT title FROM pages WHERE id = $1), last_known_title)
		 WHERE target_id = $2`, id, id); err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "cache page_links titles failed"}
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE id = $1`, id)
	if err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "delete page failed"}
	}
	n, err := res.RowsAffected()
	if err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "delete page: rows affected failed"}
	}
	if n == 0 {
		return &apiErr{http.StatusNotFound, "not_found", "page not found"}
	}

	// Explicitly clear outgoing rows for the deleted source — no FK / no
	// triggers; nothing else would remove them.
	if _, err := tx.ExecContext(ctx, `DELETE FROM page_links WHERE source_id = $1`, id); err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "delete page_links source rows failed"}
	}

	if err := tx.Commit(); err != nil {
		return &apiErr{http.StatusInternalServerError, "internal", "commit failed"}
	}
	return nil
}

// pageMoveParams is the normalized move request shared by the REST MovePage
// handler and the MCP move_page tool. Each "Set" flag distinguishes "field
// omitted" from "field provided" so parent_id can be set to null (detach to
// root) distinctly from "leave parent unchanged".
type pageMoveParams struct {
	SpaceIDSet bool
	NewSpaceID int64

	ParentIDSet    bool
	ParentIDIsNull bool
	NewParentID    int64

	PositionSet bool
	NewPosition int64
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

	var mv pageMoveParams
	if v, ok := rawMap["space_id"]; ok {
		mv.SpaceIDSet = true
		if err := json.Unmarshal(v, &mv.NewSpaceID); err != nil || mv.NewSpaceID <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_space_id", "space_id must be a positive integer")
			return
		}
	}
	if v, ok := rawMap["parent_id"]; ok {
		mv.ParentIDSet = true
		if string(v) == "null" {
			mv.ParentIDIsNull = true
		} else if err := json.Unmarshal(v, &mv.NewParentID); err != nil || mv.NewParentID <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_parent_id", "parent_id must be a positive integer or null")
			return
		}
	}
	if v, ok := rawMap["position"]; ok {
		mv.PositionSet = true
		if err := json.Unmarshal(v, &mv.NewPosition); err != nil || mv.NewPosition < 0 {
			writeError(w, http.StatusBadRequest, "invalid_position", "position must be a non-negative integer")
			return
		}
	}

	k, _ := auth.APIKeyFromContext(r.Context())
	moved, ae := s.movePageCore(r.Context(), u, k, id, mv)
	if ae != nil {
		writeError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": moved})
}

// movePageCore is the transport-agnostic core behind POST /api/pages/{id}/move
// and the MCP move_page tool: reparent / reorder / relocate a page (and its
// subtree) under editor+ membership in both the source and target space, with
// cycle detection and sibling renumbering.
func (s *Server) movePageCore(ctx context.Context, u *auth.User, k *auth.APIKey, id int64, mv pageMoveParams) (models.Page, *apiErr) {
	if !mv.SpaceIDSet && !mv.ParentIDSet && !mv.PositionSet {
		return models.Page{}, &apiErr{http.StatusBadRequest, "no_fields", "at least one of space_id, parent_id, position must be provided"}
	}
	// Guard here (not just in the REST parser) so the MCP path can't reach the
	// slice math below with a negative position and panic (newSiblingIDs[:insertAt]).
	if mv.PositionSet && mv.NewPosition < 0 {
		return models.Page{}, &apiErr{http.StatusBadRequest, "invalid_position", "position must be a non-negative integer"}
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "begin tx failed"}
	}
	defer tx.Rollback()

	page, err := selectPageByIDTx(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Page{}, &apiErr{http.StatusForbidden, "forbidden", "not a member"}
	}
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "lookup page failed"}
	}
	if ae := apiKeySpaceScopeErr(k, page.SpaceID); ae != nil {
		return models.Page{}, ae
	}

	sourceRole, err := spaceRoleTx(ctx, tx, u.ID, page.SpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Page{}, &apiErr{http.StatusForbidden, "forbidden", "not a member"}
	}
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "lookup membership failed"}
	}
	if !canEdit(sourceRole) {
		return models.Page{}, &apiErr{http.StatusForbidden, "forbidden", "editor or owner role required"}
	}

	targetSpaceID := page.SpaceID
	if mv.SpaceIDSet {
		targetSpaceID = mv.NewSpaceID
		if err := verifySpaceExistsTx(ctx, tx, targetSpaceID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return models.Page{}, &apiErr{http.StatusBadRequest, "space_not_found", "target space does not exist"}
			}
			return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "lookup target space failed"}
		}
		if targetSpaceID != page.SpaceID {
			if ae := apiKeySpaceScopeErr(k, targetSpaceID); ae != nil {
				return models.Page{}, ae
			}
			targetRole, err := spaceRoleTx(ctx, tx, u.ID, targetSpaceID)
			if errors.Is(err, sql.ErrNoRows) {
				return models.Page{}, &apiErr{http.StatusForbidden, "forbidden", "not a member of target space"}
			}
			if err != nil {
				return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "lookup target membership failed"}
			}
			if !canEdit(targetRole) {
				return models.Page{}, &apiErr{http.StatusForbidden, "forbidden", "editor or owner role required in target space"}
			}
		}
	}

	var targetParentID *int64
	if mv.ParentIDSet {
		if !mv.ParentIDIsNull {
			parent := mv.NewParentID
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
			return models.Page{}, &apiErr{http.StatusBadRequest, "parent_not_found", "parent page does not exist"}
		}
		if err != nil {
			return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "lookup parent failed"}
		}
		if parentSpaceID != targetSpaceID {
			return models.Page{}, &apiErr{http.StatusBadRequest, "parent_space_mismatch", "parent page is in a different space"}
		}
		if cyclic, err := wouldCreateCycle(ctx, tx, id, *targetParentID); err != nil {
			return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "cycle check failed"}
		} else if cyclic {
			return models.Page{}, &apiErr{http.StatusBadRequest, "cycle", "move would create a cycle"}
		}
	}

	sameGroup := page.SpaceID == targetSpaceID && parentIDPtrEqual(page.ParentID, targetParentID)

	newSiblingIDs, err := siblingIDsExcluding(ctx, tx, targetSpaceID, targetParentID, id)
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "list new siblings failed"}
	}

	insertAt := int64(len(newSiblingIDs))
	if mv.PositionSet && mv.NewPosition < insertAt {
		insertAt = mv.NewPosition
	}

	finalList := make([]int64, 0, len(newSiblingIDs)+1)
	finalList = append(finalList, newSiblingIDs[:insertAt]...)
	finalList = append(finalList, id)
	finalList = append(finalList, newSiblingIDs[insertAt:]...)

	if _, err := tx.ExecContext(ctx,
		`UPDATE pages SET space_id = $1, parent_id = $2, updated_at = tela_now() WHERE id = $3`,
		targetSpaceID, nullableInt64(targetParentID), id); err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "move page failed"}
	}

	if page.SpaceID != targetSpaceID {
		if err := updateDescendantsSpaceID(ctx, tx, id, targetSpaceID); err != nil {
			return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "propagate space_id to descendants failed"}
		}
	}

	for i, sid := range finalList {
		if _, err := tx.ExecContext(ctx,
			`UPDATE pages SET position = $1 WHERE id = $2`, int64(i), sid); err != nil {
			return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "renumber new siblings failed"}
		}
	}

	if !sameGroup {
		oldSiblingIDs, err := siblingIDsExcluding(ctx, tx, page.SpaceID, page.ParentID, id)
		if err != nil {
			return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "list old siblings failed"}
		}
		for i, sid := range oldSiblingIDs {
			if _, err := tx.ExecContext(ctx,
				`UPDATE pages SET position = $1 WHERE id = $2`, int64(i), sid); err != nil {
				return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "renumber old siblings failed"}
			}
		}
	}

	moved, err := selectPageByIDTx(ctx, tx, id)
	if err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "fetch moved page failed"}
	}
	if err := tx.Commit(); err != nil {
		return models.Page{}, &apiErr{http.StatusInternalServerError, "internal", "commit failed"}
	}
	return moved, nil
}

func buildPageTree(ctx context.Context, db *sql.DB, spaceID int64) ([]*pageNode, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, space_id, parent_id, title, body, position, props, created_at, updated_at
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
		`SELECT id, space_id, parent_id, title, body, position, props, created_at, updated_at
		 FROM pages WHERE id = $1`, id)
	return scanPageFromRow(row)
}

func selectPageByIDTx(ctx context.Context, tx *sql.Tx, id int64) (models.Page, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT id, space_id, parent_id, title, body, position, props, created_at, updated_at
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
	var propsRaw []byte
	if err := r.Scan(&p.ID, &p.SpaceID, &parentID, &p.Title, &p.Body, &p.Position, &propsRaw, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return p, err
	}
	if parentID.Valid {
		v := parentID.Int64
		p.ParentID = &v
	}
	p.Props = map[string]any{}
	if len(propsRaw) > 0 {
		if err := json.Unmarshal(propsRaw, &p.Props); err != nil {
			return p, fmt.Errorf("scan page props: %w", err)
		}
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

// wikiBracketRE matches Obsidian-style `[[Name]]` wikilinks (Name has no nested
// brackets). `[[Name|alias]]` and `[[Name#heading]]` are supported — the alias /
// heading suffix is trimmed before resolution.
var wikiBracketRE = regexp.MustCompile(`\[\[([^\[\]]+?)\]\]`)

// parseWikiTitleSlugs extracts `[[Name]]` wikilink names from body and reduces
// each to a page slug, so `[[Route Analyze]]`, `[[route-analyze]]` and
// `[[route-analyze|alias]]` all normalise to "route-analyze". Returns unique,
// non-empty slugs; the canonical `tela://page/{id}` links are parsed separately
// by parseWikiLinks.
func parseWikiTitleSlugs(body string) []string {
	matches := wikiBracketRE.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		name := m[1]
		if i := strings.IndexAny(name, "|#"); i >= 0 {
			name = name[:i]
		}
		s := pageSlug(name)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// resolveWikiTitleSlugs maps each `[[Name]]` slug to a target page id within
// sourceID's space, matching on the slug-normalised page title (lowest id wins
// on a title clash). Resolution is space-scoped so a name can't link across a
// membership boundary; names that match no page are dropped (nothing to link).
func resolveWikiTitleSlugs(ctx context.Context, tx *sql.Tx, sourceID int64, slugs []string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, title FROM pages
		WHERE space_id = (SELECT space_id FROM pages WHERE id = $1)
		ORDER BY id ASC`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("load space pages for wikilink resolution: %w", err)
	}
	defer rows.Close()
	bySlug := make(map[string]int64)
	for rows.Next() {
		var id int64
		var title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, fmt.Errorf("scan page for wikilink resolution: %w", err)
		}
		if s := pageSlug(title); s != "" {
			if _, ok := bySlug[s]; !ok { // ORDER BY id ASC → lowest id wins
				bySlug[s] = id
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pages for wikilink resolution: %w", err)
	}
	out := make([]int64, 0, len(slugs))
	for _, s := range slugs {
		if id, ok := bySlug[s]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// syncPageLinks rebuilds the outgoing page_links rows for sourceID from
// body: deletes existing rows, then inserts one row per unique wikilink
// target. Targets come from canonical `tela://page/{id}` links (parseWikiLinks)
// plus Obsidian-style `[[Name]]` links resolved by title within the same space.
// last_known_title is the live target title, or an empty string when the target
// does not exist — that's how a freshly broken link is recorded.
func syncPageLinks(ctx context.Context, tx *sql.Tx, sourceID int64, body string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM page_links WHERE source_id = $1`, sourceID); err != nil {
		return fmt.Errorf("delete outgoing page_links: %w", err)
	}
	targets := parseWikiLinks(body)
	if slugs := parseWikiTitleSlugs(body); len(slugs) > 0 {
		resolved, err := resolveWikiTitleSlugs(ctx, tx, sourceID, slugs)
		if err != nil {
			return err
		}
		seen := make(map[int64]struct{}, len(targets))
		for _, id := range targets {
			seen[id] = struct{}{}
		}
		for _, id := range resolved {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				targets = append(targets, id)
			}
		}
	}
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
