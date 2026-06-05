package rag

import (
	"context"
	"testing"
	"time"

	"github.com/zcag/tela/backend/internal/testdb"
)

func TestFreshness_StatusesAndRollup(t *testing.T) {
	d := testdb.New(t)
	ctx := context.Background()
	u := newUser(t, d, "alice")
	sp := newSpace(t, d, "alpha", u)

	indexed := newPage(t, d, sp, "Indexed", "## A\nsome real content to chunk")
	empty := newPage(t, d, sp, "Empty", "   ")
	unindexed := newPage(t, d, sp, "Unindexed", "## B\nnever indexed content")
	stale := newPage(t, d, sp, "Stale", "## C\noriginal content")

	svc := NewServiceWithEmbedder(d, &fakeEmbedder{})

	// Index only `indexed` and `stale`.
	if _, err := svc.ReindexPage(ctx, indexed); err != nil {
		t.Fatalf("index indexed: %v", err)
	}
	if _, err := svc.ReindexPage(ctx, stale); err != nil {
		t.Fatalf("index stale: %v", err)
	}
	// Now edit `stale` so its updated_at moves past its chunks' index time.
	if _, err := d.Exec(`UPDATE pages SET body = $1, updated_at = $2 WHERE id = $3`,
		"## C\nedited content after indexing", "2099-01-01 00:00:00", stale); err != nil {
		t.Fatalf("touch stale: %v", err)
	}

	// Per-page statuses.
	pages, err := svc.SpacePageFreshness(ctx, u, sp)
	if err != nil {
		t.Fatalf("page freshness: %v", err)
	}
	got := map[int64]string{}
	for _, p := range pages {
		got[p.PageID] = p.Status
	}
	want := map[int64]string{indexed: "fresh", empty: "empty", unindexed: "unindexed", stale: "stale"}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("page %d status = %q, want %q", id, got[id], w)
		}
	}

	// Space rollup: 4 pages, 2 indexed (have chunks), stale_pages counts the
	// edited-after-index one + the never-indexed one (both non-empty, out of date).
	spaces, err := svc.Freshness(ctx, u)
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	if len(spaces) != 1 {
		t.Fatalf("want 1 space, got %d", len(spaces))
	}
	f := spaces[0]
	if f.Pages != 4 {
		t.Errorf("pages = %d, want 4", f.Pages)
	}
	if f.IndexedPages != 2 {
		t.Errorf("indexed_pages = %d, want 2", f.IndexedPages)
	}
	if f.StalePages != 2 {
		t.Errorf("stale_pages = %d, want 2 (unindexed + edited-after-index)", f.StalePages)
	}
	if f.ChunkCount == 0 || f.LastIndexed == "" {
		t.Errorf("expected chunks + last_indexed, got count=%d last=%q", f.ChunkCount, f.LastIndexed)
	}
}

func TestFreshness_ScopedToAccess(t *testing.T) {
	d := testdb.New(t)
	ctx := context.Background()
	alice := newUser(t, d, "alice")
	bob := newUser(t, d, "bob")
	_ = newSpace(t, d, "alpha", alice)
	bobSpace := newSpace(t, d, "bravo", bob)
	service := NewServiceWithEmbedder(d, &fakeEmbedder{})

	// alice sees only her space.
	spaces, err := service.Freshness(ctx, alice)
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	for _, f := range spaces {
		if f.SpaceID == bobSpace {
			t.Fatalf("LEAK: alice saw bob's space %d in freshness", bobSpace)
		}
	}
	// And per-page on bob's space returns empty for alice.
	pages, err := service.SpacePageFreshness(ctx, alice, bobSpace)
	if err != nil {
		t.Fatalf("page freshness: %v", err)
	}
	if len(pages) != 0 {
		t.Fatalf("LEAK: alice got %d pages of bob's space", len(pages))
	}
}

func TestAutoReindex_DebouncedWorker(t *testing.T) {
	// Shrink the windows so the worker fires in milliseconds, not seconds.
	origDebounce, origTick := reindexDebounce, reindexTick
	reindexDebounce, reindexTick = 15*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { reindexDebounce, reindexTick = origDebounce, origTick })

	d := testdb.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	u := newUser(t, d, "carol")
	sp := newSpace(t, d, "charlie", u)
	page := newPage(t, d, sp, "Doc", "## A\ncontent to be auto-indexed")

	service := NewServiceWithEmbedder(d, &fakeEmbedder{})
	service.StartAutoReindex(ctx)
	service.QueueReindex(page)

	// Poll for chunks to appear (worker reindexes after the debounce).
	deadline := time.Now().Add(2 * time.Second)
	var count int
	for time.Now().Before(deadline) {
		if err := d.QueryRow(`SELECT count(*) FROM page_chunks WHERE page_id = $1`, page).Scan(&count); err != nil {
			t.Fatalf("count chunks: %v", err)
		}
		if count > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("auto-reindex did not produce chunks for page %d within deadline", page)
}
