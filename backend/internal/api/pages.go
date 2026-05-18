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
// so the tree endpoint can return a nested structure.
type pageNode struct {
	models.Page
	Children []*pageNode `json:"children"`
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
			 FROM pages WHERE space_id = ? AND parent_id IS NULL
			 ORDER BY position ASC, id ASC`, spaceID)
	default:
		parentID, perr := strconv.ParseInt(parentIDStr, 10, 64)
		if perr != nil || parentID <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_query", "parent_id must be a positive integer or 'null'")
			return
		}
		rows, err = s.DB.QueryContext(r.Context(),
			`SELECT id, space_id, parent_id, title, body, position, created_at, updated_at
			 FROM pages WHERE space_id = ? AND parent_id = ?
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
	writeJSON(w, http.StatusOK, map[string]any{"pages": pages})
}

func (s *Server) CreatePage(w http.ResponseWriter, r *http.Request) {
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
			`SELECT space_id FROM pages WHERE id = ?`, *req.ParentID).Scan(&parentSpaceID)
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
			`SELECT MAX(position) FROM pages WHERE space_id = ? AND parent_id IS NULL`, req.SpaceID).Scan(&maxPos)
	} else {
		err = tx.QueryRowContext(ctx,
			`SELECT MAX(position) FROM pages WHERE space_id = ? AND parent_id = ?`, req.SpaceID, *req.ParentID).Scan(&maxPos)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "compute position failed")
		return
	}
	var position int64
	if maxPos.Valid {
		position = maxPos.Int64 + 1
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO pages(space_id, parent_id, title, body, position) VALUES (?, ?, ?, ?, ?)`,
		req.SpaceID, nullableInt64(req.ParentID), title, req.Body, position)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create page failed")
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create page: last insert id failed")
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
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch page failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": p})
}

func (s *Server) UpdatePage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
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
		sets = append(sets, "title = ?")
		args = append(args, title)
	}
	if req.Body != nil {
		sets = append(sets, "body = ?")
		args = append(args, *req.Body)
	}
	sets = append(sets, "updated_at = datetime('now')")
	args = append(args, id)

	stmt := "UPDATE pages SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	res, err := s.DB.ExecContext(r.Context(), stmt, args...)
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
	p, err := selectPageByID(r.Context(), s.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "fetch updated page failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": p})
}

func (s *Server) DeletePage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id")
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

	res, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE id = ?`, id)
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
		writeError(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup page failed")
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
			`SELECT space_id FROM pages WHERE id = ?`, *targetParentID).Scan(&parentSpaceID)
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
		`UPDATE pages SET space_id = ?, parent_id = ?, updated_at = datetime('now') WHERE id = ?`,
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
			`UPDATE pages SET position = ? WHERE id = ?`, int64(i), sid); err != nil {
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
				`UPDATE pages SET position = ? WHERE id = ?`, int64(i), sid); err != nil {
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
		 FROM pages WHERE space_id = ?
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
		 FROM pages WHERE id = ?`, id)
	return scanPageFromRow(row)
}

func selectPageByIDTx(ctx context.Context, tx *sql.Tx, id int64) (models.Page, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT id, space_id, parent_id, title, body, position, created_at, updated_at
		 FROM pages WHERE id = ?`, id)
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
	return db.QueryRowContext(ctx, `SELECT 1 FROM spaces WHERE id = ?`, id).Scan(&x)
}

func verifySpaceExistsTx(ctx context.Context, tx *sql.Tx, id int64) error {
	var x int
	return tx.QueryRowContext(ctx, `SELECT 1 FROM spaces WHERE id = ?`, id).Scan(&x)
}

func siblingIDsExcluding(ctx context.Context, tx *sql.Tx, spaceID int64, parentID *int64, excludeID int64) ([]int64, error) {
	var rows *sql.Rows
	var err error
	if parentID == nil {
		rows, err = tx.QueryContext(ctx,
			`SELECT id FROM pages WHERE space_id = ? AND parent_id IS NULL AND id != ?
			 ORDER BY position ASC, id ASC`, spaceID, excludeID)
	} else {
		rows, err = tx.QueryContext(ctx,
			`SELECT id FROM pages WHERE space_id = ? AND parent_id = ? AND id != ?
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
		err := tx.QueryRowContext(ctx, `SELECT parent_id FROM pages WHERE id = ?`, cursor).Scan(&pid)
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
			placeholders[i] = "?"
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
			updPlaceholders[i] = "?"
			updArgs = append(updArgs, nid)
		}
		upd := fmt.Sprintf(`UPDATE pages SET space_id = ? WHERE id IN (%s)`, strings.Join(updPlaceholders, ","))
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
