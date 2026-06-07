package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/mdimport"
	"github.com/zcag/tela/backend/internal/models"
	"github.com/zcag/tela/backend/internal/pagemd"
)

// syncAction names how one incoming file resolved against the store.
type syncAction string

const (
	syncUnchanged   syncAction = "unchanged"   // idempotent no-op: nothing differed
	syncCreated     syncAction = "created"     // new page (a fresh id was assigned)
	syncUpdated     syncAction = "updated"     // existing page, content changed in place
	syncMoved       syncAction = "moved"       // existing page, reparented/relocated (± content)
	syncResurrected syncAction = "resurrected" // a soft-deleted page brought back by its id
)

// ApplyFileSync is the id-aware, idempotent sync ingress: it resolves one
// incoming markdown file to a create / update / move against the page named by
// its frontmatter `id`, then applies it through the existing page-core funcs.
//
// Unlike mdimport.Import (create-only, which renames on a title clash), this
// binds by `id`, so re-applying the same file is a true no-op — no duplicate
// page, no spurious revision, no updated_at churn. That idempotency is what
// keeps a polling client from ping-ponging a file every cycle.
//
// Placement (spaceID, parentID) comes from the file's LOCATION (the WebDAV layer
// derives it from the path, later); identity comes from `id`. When the two
// disagree it is a move, not a new page. This is the transport-agnostic core the
// WebDAV PUT handler will call — it does no auth of its own beyond what the
// page-core funcs already enforce (membership + edit role on the target space).
func (s *Server) ApplyFileSync(
	ctx context.Context, u *auth.User, k *auth.APIKey,
	spaceID int64, parentID *int64, filename string, content []byte,
) (models.Page, syncAction, *apiErr) {
	d := pagemd.DecodeDoc(pagemd.NormalizeText(string(content)))
	title := mdimport.TitleFor(d.Title, d.Body, filename)
	props := d.Props
	if props == nil {
		props = map[string]any{}
	}

	// Bind by id when present and still resolvable. A missing/unknown id falls
	// through to CREATE (a fresh id is assigned); resurrecting a soft-deleted
	// page by its old id is deferred to the delete-safety work.
	if d.ID != nil {
		existing, err := selectPageByID(ctx, s.DB, *d.ID)
		if err == nil {
			return s.applySyncBound(ctx, u, k, existing, spaceID, parentID, title, d.Body, props)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "lookup page failed"}
		}
		// Live miss — the id may belong to a soft-deleted page; a re-synced file
		// resurrects it rather than minting a duplicate. handled=false means no
		// trashed page owns this id either → a genuinely unknown id → create.
		if p, act, handled, ae := s.applySyncResurrect(ctx, u, k, *d.ID, spaceID, parentID, title, d.Body, props); handled || ae != nil {
			return p, act, ae
		}
	} else {
		// No frontmatter id, but a sibling page may already occupy this filename
		// slug at the target location → bind to it (update in place) instead of
		// minting a duplicate. This is what makes `<slug>.md` idempotent against
		// the page its `<slug>/` directory represents (so MKCOL-then-PUT-index
		// doesn't create a second page), and it stops a re-push — before the
		// server-assigned id has round-tripped back into the file — from forking
		// the page. Identity is still path-derived only as a last resort: a file
		// that carries an id always binds by id above.
		if existing, ok, err := s.findSiblingByFilename(ctx, spaceID, parentID, filename); err != nil {
			return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "lookup sibling failed"}
		} else if ok {
			return s.applySyncBound(ctx, u, k, existing, spaceID, parentID, title, d.Body, props)
		}
	}

	p, ae := s.createPageCore(ctx, u, k, pageCreateRequest{
		SpaceID: spaceID, ParentID: parentID, Title: title, Body: d.Body, Props: props,
	})
	if ae != nil {
		return models.Page{}, "", ae
	}
	return p, syncCreated, nil
}

// applySyncResurrect brings a soft-deleted page back when a re-synced file
// carries its id (sync §6 — the resurrect edge). All in one tx: confirm the id
// belongs to a trashed page, authorize edit on its space, clear deleted_at, then
// apply the incoming content (+ move) via the shared primitives. handled=false
// (no error) means no trashed page owns this id — the caller falls through to a
// fresh create. A live-but-not-trashed race returns 409 (self-heals next cycle).
func (s *Server) applySyncResurrect(
	ctx context.Context, u *auth.User, k *auth.APIKey, id int64,
	spaceID int64, parentID *int64, title, body string, props map[string]any,
) (models.Page, syncAction, bool, *apiErr) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return models.Page{}, "", true, &apiErr{http.StatusInternalServerError, "internal", "begin tx failed"}
	}
	defer tx.Rollback()

	var curSpace int64
	err = tx.QueryRowContext(ctx,
		`SELECT space_id FROM pages WHERE id = $1 AND deleted_at IS NOT NULL`, id).Scan(&curSpace)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Page{}, "", false, nil // not trashed → caller creates fresh
	}
	if err != nil {
		return models.Page{}, "", true, &apiErr{http.StatusInternalServerError, "internal", "lookup trashed page failed"}
	}
	if ae := s.requireEditTx(ctx, tx, u, k, curSpace); ae != nil {
		return models.Page{}, "", true, ae // auth fail → rollback leaves it trashed
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE pages SET deleted_at = NULL, updated_at = tela_now() WHERE id = $1`, id); err != nil {
		return models.Page{}, "", true, &apiErr{http.StatusInternalServerError, "internal", "resurrect failed"}
	}
	cur, err := selectPageByIDTx(ctx, tx, id) // now live (own write)
	if err != nil {
		return models.Page{}, "", true, &apiErr{http.StatusInternalServerError, "internal", "fetch resurrected page failed"}
	}

	req := pageUpdateRequest{Title: &title, Body: &body, Props: props}
	if ae := validateUpdateReq(req); ae != nil {
		return models.Page{}, "", true, ae
	}
	p, ae := applyUpdateTx(ctx, tx, id, req)
	if ae != nil {
		return models.Page{}, "", true, ae
	}
	if !syncPlacementSame(cur, spaceID, parentID) {
		moved, ae := s.applyMoveTx(ctx, tx, u, k, p, syncMoveParams(cur, spaceID, parentID))
		if ae != nil {
			return models.Page{}, "", true, ae
		}
		p = moved
	}
	if err := tx.Commit(); err != nil {
		return models.Page{}, "", true, &apiErr{http.StatusInternalServerError, "internal", "commit failed"}
	}
	// cur carries the page's pre-resurrect body, so afterPageWrite snapshots a
	// revision + reindexes + resets the overlay whenever the returning content
	// differs (DB-wins, like any sync write).
	s.afterPageWrite(ctx, cur, p, true, true, u.ID, "sync")
	return p, syncResurrected, true, nil
}

// applySyncBound applies an incoming file to the existing page it is bound to.
// The common steady-state case — nothing differs — is a tx-free fast path off
// the already-fetched row. When something did change, the whole reconcile runs
// in ONE transaction: re-fetch (so the change decision and the write are atomic
// and race-safe against a concurrent edit), authorize once, then update and/or
// move via the shared in-tx primitives. Content and placement are reconciled
// independently, so we never write (or snapshot/reindex/reset-overlay) a
// dimension the file did not actually change.
func (s *Server) applySyncBound(
	ctx context.Context, u *auth.User, k *auth.APIKey,
	existing models.Page, spaceID int64, parentID *int64,
	title, body string, props map[string]any,
) (models.Page, syncAction, *apiErr) {
	if syncContentSame(existing, title, body, props) && syncPlacementSame(existing, spaceID, parentID) {
		return existing, syncUnchanged, nil // fast path: no tx, no write
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "begin tx failed"}
	}
	defer tx.Rollback()

	// Re-read under the tx; recompute against the fresh row so a concurrent write
	// that already landed our content is a no-op rather than a clobber.
	cur, err := selectPageByIDTx(ctx, tx, existing.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Page{}, "", &apiErr{http.StatusNotFound, "not_found", "page vanished mid-sync"}
	}
	if err != nil {
		return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "lookup page failed"}
	}
	if ae := s.requireEditTx(ctx, tx, u, k, cur.SpaceID); ae != nil {
		return models.Page{}, "", ae
	}

	contentChanged := !syncContentSame(cur, title, body, props)
	placementChanged := !syncPlacementSame(cur, spaceID, parentID)
	if !contentChanged && !placementChanged {
		return cur, syncUnchanged, nil // a concurrent write beat us to it
	}

	p := cur
	action := syncUpdated
	if contentChanged {
		req := pageUpdateRequest{Title: &title, Body: &body, Props: props}
		if ae := validateUpdateReq(req); ae != nil {
			return models.Page{}, "", ae
		}
		updated, ae := applyUpdateTx(ctx, tx, cur.ID, req)
		if ae != nil {
			return models.Page{}, "", ae
		}
		p = updated
	}
	if placementChanged {
		moved, ae := s.applyMoveTx(ctx, tx, u, k, p, syncMoveParams(cur, spaceID, parentID))
		if ae != nil {
			return models.Page{}, "", ae
		}
		p, action = moved, syncMoved
	}
	if err := tx.Commit(); err != nil {
		return models.Page{}, "", &apiErr{http.StatusInternalServerError, "internal", "commit failed"}
	}

	// agentWrite=true: a sync write is out-of-band w.r.t. any live Yjs overlay,
	// so the overlay must re-seed from the new body (DB-wins), like an agent
	// write. Only fires when body/title actually changed.
	if contentChanged {
		s.afterPageWrite(ctx, cur, p, true, true, u.ID, "sync")
	}
	return p, action, nil
}

// syncContentSame reports whether the file's title/body/props already match the
// page (so no update is needed).
func syncContentSame(p models.Page, title, body string, props map[string]any) bool {
	return title == p.Title && body == p.Body && propsEqual(props, p.Props)
}

// syncPlacementSame reports whether the file's location already matches the
// page's space + parent (so no move is needed).
func syncPlacementSame(p models.Page, spaceID int64, parentID *int64) bool {
	return p.SpaceID == spaceID && sameParent(p.ParentID, parentID)
}

// syncMoveParams builds the move request that relocates a page to the file's
// location (space + parent derived from its path).
func syncMoveParams(cur models.Page, spaceID int64, parentID *int64) pageMoveParams {
	mv := pageMoveParams{ParentIDSet: true}
	if cur.SpaceID != spaceID {
		mv.SpaceIDSet = true
		mv.NewSpaceID = spaceID
	}
	if parentID == nil {
		mv.ParentIDIsNull = true
	} else {
		mv.NewParentID = *parentID
	}
	return mv
}

// propsEqual compares two props bags by canonical JSON so a yaml-parsed int and
// a JSONB-stored float (same value) are treated as equal — otherwise a benign
// type difference would read as a change on every sync. Both bags are non-nil
// here (callers default to {}); key order is normalized by json.Marshal.
func propsEqual(a, b map[string]any) bool {
	ja, err1 := json.Marshal(a)
	jb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(ja, jb)
}

// findSiblingByFilename locates the live page that would be written as filename
// under (spaceID, parentID) — i.e. the sibling whose deduped on-disk slug equals
// the file's stem. It powers ApplyFileSync's path-fallback bind for id-less
// files (see the call site). Returns ok=false when nothing matches (a genuinely
// new file → create). One indexed query over the sibling group; the dedup uses
// the same siblingSlugs mapping the read side emits, so the match is exact and
// reciprocal with what a client last pulled.
func (s *Server) findSiblingByFilename(ctx context.Context, spaceID int64, parentID *int64, filename string) (models.Page, bool, error) {
	stem := strings.TrimSuffix(filename, ".md")
	if stem == "" {
		return models.Page{}, false, nil
	}
	var (
		rows *sql.Rows
		err  error
	)
	const cols = `id, space_id, parent_id, title, body, position, props, created_at, updated_at`
	if parentID == nil {
		rows, err = s.DB.QueryContext(ctx,
			`SELECT `+cols+` FROM pages
			  WHERE space_id = $1 AND parent_id IS NULL AND deleted_at IS NULL
			  ORDER BY position ASC, id ASC`, spaceID)
	} else {
		rows, err = s.DB.QueryContext(ctx,
			`SELECT `+cols+` FROM pages
			  WHERE space_id = $1 AND parent_id = $2 AND deleted_at IS NULL
			  ORDER BY position ASC, id ASC`, spaceID, *parentID)
	}
	if err != nil {
		return models.Page{}, false, err
	}
	defer rows.Close()
	var siblings []models.Page
	for rows.Next() {
		p, err := scanPageFromRows(rows)
		if err != nil {
			return models.Page{}, false, err
		}
		siblings = append(siblings, p)
	}
	if err := rows.Err(); err != nil {
		return models.Page{}, false, err
	}
	slugs := siblingSlugs(siblings)
	for _, p := range siblings {
		if slugs[p.ID] == stem {
			return p, true, nil
		}
	}
	return models.Page{}, false, nil
}

// sameParent reports whether two nullable parent ids are equal (both nil = both
// at space root).
func sameParent(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
