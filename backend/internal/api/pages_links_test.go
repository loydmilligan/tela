package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/zcag/tela/backend/internal/testdb"
)

// TestSyncPageLinks_SkipsSelfLink: a body that links to its own page id must
// not produce a self-row in page_links. Audit #7 from the M5.2 refactorer
// pass — would otherwise render as "this page links to itself" in backlinks.
func TestSyncPageLinks_SkipsSelfLink(t *testing.T) {
	ctx := context.Background()
	d := testdb.New(t)

	if _, err := d.ExecContext(ctx, `INSERT INTO spaces(name, slug) VALUES ($1,$2)`, "General", "general"); err != nil {
		t.Fatalf("seed space: %v", err)
	}
	var spaceID int64
	if err := d.QueryRowContext(ctx, `SELECT id FROM spaces WHERE slug = 'general'`).Scan(&spaceID); err != nil {
		t.Fatalf("read space id: %v", err)
	}

	// Insert page A and page B in the same space.
	var pageA int64
	if err := d.QueryRowContext(ctx, `INSERT INTO pages(space_id, parent_id, title, body, position) VALUES ($1, NULL, $2, '', 0) RETURNING id`, spaceID, "Page A").Scan(&pageA); err != nil {
		t.Fatalf("insert page A: %v", err)
	}
	var pageB int64
	if err := d.QueryRowContext(ctx, `INSERT INTO pages(space_id, parent_id, title, body, position) VALUES ($1, NULL, $2, '', 1) RETURNING id`, spaceID, "Page B").Scan(&pageB); err != nil {
		t.Fatalf("insert page B: %v", err)
	}

	// Body links to self twice (different forms) and to B once. Only the B row
	// should land in page_links.
	body := fmt.Sprintf("See tela://page/%d and also [back to me](tela://page/%d) and [B](tela://page/%d).", pageA, pageA, pageB)

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := syncPageLinks(ctx, tx, pageA, body); err != nil {
		tx.Rollback()
		t.Fatalf("syncPageLinks: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var selfCount int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM page_links WHERE source_id = $1 AND target_id = $2`, pageA, pageA).Scan(&selfCount); err != nil {
		t.Fatalf("count self rows: %v", err)
	}
	if selfCount != 0 {
		t.Fatalf("self-link rows = %d, want 0", selfCount)
	}

	var bCount int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM page_links WHERE source_id = $1 AND target_id = $2`, pageA, pageB).Scan(&bCount); err != nil {
		t.Fatalf("count B rows: %v", err)
	}
	if bCount != 1 {
		t.Fatalf("link-to-B rows = %d, want 1", bCount)
	}

	// Now test the pure-self-link case: only self-refs in body → zero rows.
	bodyOnlySelf := fmt.Sprintf("tela://page/%d and again tela://page/%d", pageA, pageA)
	tx, err = d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx (only-self): %v", err)
	}
	if err := syncPageLinks(ctx, tx, pageA, bodyOnlySelf); err != nil {
		tx.Rollback()
		t.Fatalf("syncPageLinks (only-self): %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit (only-self): %v", err)
	}

	var totalForA int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM page_links WHERE source_id = $1`, pageA).Scan(&totalForA); err != nil {
		t.Fatalf("count total for A: %v", err)
	}
	if totalForA != 0 {
		t.Fatalf("page_links rows for A after pure-self body = %d, want 0", totalForA)
	}
}

// TestSyncPageLinks_BracketWikilinks: Obsidian-style [[Name]] wikilinks resolve
// to a backlink by slug-normalised title within the same space — [[Route Analyze]]
// and [[route-analyze|alias]] both link to a page titled "Route Analyze", a name
// with no matching page is dropped, and resolution does not cross into another
// space.
func TestSyncPageLinks_BracketWikilinks(t *testing.T) {
	ctx := context.Background()
	d := testdb.New(t)

	mkSpace := func(slug string) int64 {
		if _, err := d.ExecContext(ctx, `INSERT INTO spaces(name, slug) VALUES ($1,$2)`, slug, slug); err != nil {
			t.Fatalf("seed space %s: %v", slug, err)
		}
		var id int64
		if err := d.QueryRowContext(ctx, `SELECT id FROM spaces WHERE slug = $1`, slug).Scan(&id); err != nil {
			t.Fatalf("read space id %s: %v", slug, err)
		}
		return id
	}
	mkPage := func(spaceID int64, title string) int64 {
		var id int64
		if err := d.QueryRowContext(ctx, `INSERT INTO pages(space_id, parent_id, title, body, position) VALUES ($1, NULL, $2, '', 0) RETURNING id`, spaceID, title).Scan(&id); err != nil {
			t.Fatalf("insert page %q: %v", title, err)
		}
		return id
	}

	spaceA := mkSpace("alpha")
	spaceB := mkSpace("beta")
	target := mkPage(spaceA, "Route Analyze") // [[route-analyze]] should find this
	source := mkPage(spaceA, "Index")
	otherSpaceTarget := mkPage(spaceB, "Cross Space") // must NOT resolve from spaceA

	// Title-cased, slug-cased + aliased forms all hit `target`; a dangling name and
	// a cross-space name resolve to nothing.
	body := "See [[Route Analyze]], [[route-analyze|the analyzer]], [[does-not-exist]] and [[Cross Space]]."

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := syncPageLinks(ctx, tx, source, body); err != nil {
		tx.Rollback()
		t.Fatalf("syncPageLinks: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var targets []int64
	rows, err := d.QueryContext(ctx, `SELECT target_id FROM page_links WHERE source_id = $1 ORDER BY target_id`, source)
	if err != nil {
		t.Fatalf("query links: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		targets = append(targets, id)
	}
	if len(targets) != 1 || targets[0] != target {
		t.Fatalf("bracket wikilink targets = %v, want exactly [%d] (got cross-space %d?)", targets, target, otherSpaceTarget)
	}
}
