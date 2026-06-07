package api

import (
	"context"
	"database/sql"
	"strconv"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/models"
	"github.com/zcag/tela/backend/internal/pagemd"
)

// syncFixture wires a Server + an owner user + one space for the resolver tests.
func syncFixture(t *testing.T) (*Server, *auth.User, int64) {
	t.Helper()
	d := newAPITestDB(t)
	s := New(d)
	ownerID := seedUser(t, d, "owner", "pw-owner-123", false)
	spaceID := seedSpace(t, d, "Space", "space", ownerID)
	return s, &auth.User{ID: ownerID, Username: "owner"}, spaceID
}

func revisionCount(t *testing.T, d *sql.DB, pageID int64) int {
	t.Helper()
	var n int
	if err := d.QueryRowContext(context.Background(),
		`SELECT count(*) FROM page_revisions WHERE page_id = $1`, pageID).Scan(&n); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	return n
}

// emit renders a page the way the WebDAV read side will, so round-trip tests
// feed the resolver exactly what a client would have on disk.
func emit(p models.Page) []byte { return pagemd.Encode(p, "") }

func TestApplyFileSync_CreateThenIdempotent(t *testing.T) {
	s, u, space := syncFixture(t)
	ctx := context.Background()

	// New file, no id → CREATE; title falls back to the filename.
	p, act, ae := s.ApplyFileSync(ctx, u, nil, space, nil, "My Note.md", []byte("hello body"))
	if ae != nil {
		t.Fatalf("create: %+v", ae)
	}
	if act != syncCreated {
		t.Fatalf("action = %q, want created", act)
	}
	if p.ID == 0 || p.Title != "My Note" || p.Body != "hello body" {
		t.Fatalf("created page wrong: %+v", p)
	}

	// Re-apply the emitted file (now carrying its id) → true no-op.
	before := p.UpdatedAt
	revs := revisionCount(t, s.DB, p.ID)
	p2, act2, ae := s.ApplyFileSync(ctx, u, nil, space, nil, "My Note.md", emit(p))
	if ae != nil {
		t.Fatalf("reapply: %+v", ae)
	}
	if act2 != syncUnchanged {
		t.Fatalf("reapply action = %q, want unchanged", act2)
	}
	if p2.UpdatedAt != before {
		t.Errorf("updated_at churned on no-op: %q → %q", before, p2.UpdatedAt)
	}
	if got := revisionCount(t, s.DB, p.ID); got != revs {
		t.Errorf("no-op snapshotted a revision: %d → %d", revs, got)
	}
}

func TestApplyFileSync_UpdateBindsById(t *testing.T) {
	s, u, space := syncFixture(t)
	ctx := context.Background()

	p, _, ae := s.ApplyFileSync(ctx, u, nil, space, nil, "Doc.md", []byte("v1"))
	if ae != nil {
		t.Fatalf("create: %+v", ae)
	}

	// Same id, changed body → UPDATE in place (no duplicate page).
	p.Body = "v2 changed"
	p2, act, ae := s.ApplyFileSync(ctx, u, nil, space, nil, "Doc.md", emit(p))
	if ae != nil {
		t.Fatalf("update: %+v", ae)
	}
	if act != syncUpdated {
		t.Fatalf("action = %q, want updated", act)
	}
	if p2.ID != p.ID {
		t.Fatalf("bound to wrong page: %d != %d", p2.ID, p.ID)
	}
	if p2.Body != "v2 changed" {
		t.Fatalf("body not updated: %q", p2.Body)
	}
	if n := countPagesInSpace(t, s.DB, space); n != 1 {
		t.Fatalf("update created a duplicate: %d pages", n)
	}
}

func TestApplyFileSync_MoveByPlacement(t *testing.T) {
	s, u, space := syncFixture(t)
	ctx := context.Background()

	parent, _, ae := s.ApplyFileSync(ctx, u, nil, space, nil, "Parent.md", []byte("p"))
	if ae != nil {
		t.Fatalf("parent: %+v", ae)
	}
	child, _, ae := s.ApplyFileSync(ctx, u, nil, space, nil, "Child.md", []byte("c"))
	if ae != nil {
		t.Fatalf("child: %+v", ae)
	}

	// Same child file, now located under parent → MOVE (reparent), same id.
	moved, act, ae := s.ApplyFileSync(ctx, u, nil, space, &parent.ID, "Child.md", emit(child))
	if ae != nil {
		t.Fatalf("move: %+v", ae)
	}
	if act != syncMoved {
		t.Fatalf("action = %q, want moved", act)
	}
	if moved.ID != child.ID || moved.ParentID == nil || *moved.ParentID != parent.ID {
		t.Fatalf("not reparented: %+v", moved)
	}
}

func TestApplyFileSync_UnknownIdCreatesFresh(t *testing.T) {
	s, u, space := syncFixture(t)
	ctx := context.Background()

	// A file carrying an id that doesn't exist → treated as new (fresh id).
	content := "---\nid: 999999\ntitle: Ghost\n---\nbody"
	p, act, ae := s.ApplyFileSync(ctx, u, nil, space, nil, "ghost.md", []byte(content))
	if ae != nil {
		t.Fatalf("ghost: %+v", ae)
	}
	if act != syncCreated {
		t.Fatalf("action = %q, want created", act)
	}
	if p.ID == 999999 {
		t.Fatalf("stale id was honoured as a set: %d", p.ID)
	}
	if p.Title != "Ghost" {
		t.Fatalf("title = %q, want Ghost", p.Title)
	}
}

func TestApplyFileSync_LineEndingNoise(t *testing.T) {
	s, u, space := syncFixture(t)
	ctx := context.Background()

	p, _, ae := s.ApplyFileSync(ctx, u, nil, space, nil, "Win.md", []byte("line one\nline two"))
	if ae != nil {
		t.Fatalf("create: %+v", ae)
	}

	// Same page, but the on-disk file has a leading BOM and CRLF endings, as a
	// Windows client would write it. After normalization nothing differs → no-op.
	noisy := "\ufeff---\nid: " + strconv.FormatInt(p.ID, 10) + "\n---\nline one\r\nline two"
	_, act, ae := s.ApplyFileSync(ctx, u, nil, space, nil, "Win.md", []byte(noisy))
	if ae != nil {
		t.Fatalf("reapply: %+v", ae)
	}
	if act != syncUnchanged {
		t.Fatalf("CRLF/BOM noise read as a change: action = %q", act)
	}
}

func countPagesInSpace(t *testing.T, d *sql.DB, space int64) int {
	t.Helper()
	var n int
	if err := d.QueryRowContext(context.Background(),
		`SELECT count(*) FROM pages WHERE space_id = $1`, space).Scan(&n); err != nil {
		t.Fatalf("count pages: %v", err)
	}
	return n
}
