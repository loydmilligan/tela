package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/mdimport"
	"github.com/zcag/tela/backend/internal/models"
	"github.com/zcag/tela/backend/internal/pagemd"
)

// syncAction names how one incoming file resolved against the store.
type syncAction string

const (
	syncUnchanged syncAction = "unchanged" // idempotent no-op: nothing differed
	syncCreated   syncAction = "created"   // new page (a fresh id was assigned)
	syncUpdated   syncAction = "updated"   // existing page, content changed in place
	syncMoved     syncAction = "moved"     // existing page, reparented/relocated (± content)
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
	}

	p, ae := s.createPageCore(ctx, u, k, pageCreateRequest{
		SpaceID: spaceID, ParentID: parentID, Title: title, Body: d.Body, Props: props,
	})
	if ae != nil {
		return models.Page{}, "", ae
	}
	return p, syncCreated, nil
}

// applySyncBound applies an incoming file to the existing page it is bound to.
// Content and placement are reconciled independently: an unchanged dimension is
// left untouched so we never write (and never snapshot/reindex/reset-overlay)
// more than the file actually changed.
func (s *Server) applySyncBound(
	ctx context.Context, u *auth.User, k *auth.APIKey,
	existing models.Page, spaceID int64, parentID *int64,
	title, body string, props map[string]any,
) (models.Page, syncAction, *apiErr) {
	contentSame := title == existing.Title && body == existing.Body && propsEqual(props, existing.Props)
	placementSame := existing.SpaceID == spaceID && sameParent(existing.ParentID, parentID)
	if contentSame && placementSame {
		return existing, syncUnchanged, nil
	}

	p := existing
	if !contentSame {
		// agentWrite=true: a sync write is out-of-band w.r.t. any live Yjs
		// overlay, so the overlay must re-seed from the new body (DB-wins),
		// exactly like an MCP agent write.
		updated, ae := s.updatePageCore(ctx, u, k, existing.ID, pageUpdateRequest{
			Title: &title, Body: &body, Props: props,
		}, true)
		if ae != nil {
			return models.Page{}, "", ae
		}
		p = updated
	}

	action := syncUpdated
	if !placementSame {
		mv := pageMoveParams{ParentIDSet: true}
		if existing.SpaceID != spaceID {
			mv.SpaceIDSet = true
			mv.NewSpaceID = spaceID
		}
		if parentID == nil {
			mv.ParentIDIsNull = true
		} else {
			mv.NewParentID = *parentID
		}
		moved, ae := s.movePageCore(ctx, u, k, existing.ID, mv)
		if ae != nil {
			return models.Page{}, "", ae
		}
		p, action = moved, syncMoved
	}
	return p, action, nil
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

// sameParent reports whether two nullable parent ids are equal (both nil = both
// at space root).
func sameParent(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
