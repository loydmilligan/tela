package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/zcag/tela/backend/internal/db"
)

// TestSyncPageLinks_SkipsSelfLink: a body that links to its own page id must
// not produce a self-row in page_links. Audit #7 from the M5.2 refactorer
// pass — would otherwise render as "this page links to itself" in backlinks.
func TestSyncPageLinks_SkipsSelfLink(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := d.ExecContext(ctx, `INSERT INTO spaces(name, slug) VALUES (?,?)`, "General", "general"); err != nil {
		t.Fatalf("seed space: %v", err)
	}
	var spaceID int64
	if err := d.QueryRowContext(ctx, `SELECT id FROM spaces WHERE slug = 'general'`).Scan(&spaceID); err != nil {
		t.Fatalf("read space id: %v", err)
	}

	// Insert page A and page B in the same space.
	res, err := d.ExecContext(ctx, `INSERT INTO pages(space_id, parent_id, title, body, position) VALUES (?, NULL, ?, '', 0)`, spaceID, "Page A")
	if err != nil {
		t.Fatalf("insert page A: %v", err)
	}
	pageA, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id A: %v", err)
	}
	res, err = d.ExecContext(ctx, `INSERT INTO pages(space_id, parent_id, title, body, position) VALUES (?, NULL, ?, '', 1)`, spaceID, "Page B")
	if err != nil {
		t.Fatalf("insert page B: %v", err)
	}
	pageB, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id B: %v", err)
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
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM page_links WHERE source_id = ? AND target_id = ?`, pageA, pageA).Scan(&selfCount); err != nil {
		t.Fatalf("count self rows: %v", err)
	}
	if selfCount != 0 {
		t.Fatalf("self-link rows = %d, want 0", selfCount)
	}

	var bCount int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM page_links WHERE source_id = ? AND target_id = ?`, pageA, pageB).Scan(&bCount); err != nil {
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
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM page_links WHERE source_id = ?`, pageA).Scan(&totalForA); err != nil {
		t.Fatalf("count total for A: %v", err)
	}
	if totalForA != 0 {
		t.Fatalf("page_links rows for A after pure-self body = %d, want 0", totalForA)
	}
}
