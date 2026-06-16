package api

import (
	"context"
	"testing"
)

// TestPageAuthorAndEditor — the original author is the FIRST revision's author,
// the last editor is the LATEST revision's author; they differ when a second
// person edits.
func TestPageAuthorAndEditor(t *testing.T) {
	d := newAPITestDB(t)
	ctx := context.Background()

	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	spaceID := seedSpace(t, d, "Docs", "docs", alice)
	pageID := seedPage(t, d, spaceID, "Guide")

	// Alice creates, Bob later edits.
	if _, err := insertPageRevision(ctx, d, pageID, "v1", "Guide", nil, &alice, "create"); err != nil {
		t.Fatalf("rev1: %v", err)
	}
	if _, err := insertPageRevision(ctx, d, pageID, "v2", "Guide", nil, &bob, "edit"); err != nil {
		t.Fatalf("rev2: %v", err)
	}

	author, editor := pageAuthorAndEditor(ctx, d, pageID)
	if author != "alice" {
		t.Fatalf("author=%q want alice", author)
	}
	if editor != "bob" {
		t.Fatalf("editor=%q want bob", editor)
	}
}

// TestPageAuthorAndEditor_SingleAuthor — when one person both created and last
// edited, author == editor (the byline collapses to just the author).
func TestPageAuthorAndEditor_SingleAuthor(t *testing.T) {
	d := newAPITestDB(t)
	ctx := context.Background()

	alice := seedUser(t, d, "alice", "alicepw123", false)
	spaceID := seedSpace(t, d, "Docs", "docs", alice)
	pageID := seedPage(t, d, spaceID, "Guide")
	if _, err := insertPageRevision(ctx, d, pageID, "v1", "Guide", nil, &alice, "create"); err != nil {
		t.Fatalf("rev1: %v", err)
	}

	author, editor := pageAuthorAndEditor(ctx, d, pageID)
	if author != "alice" || editor != "alice" {
		t.Fatalf("author=%q editor=%q want alice/alice", author, editor)
	}
}

// TestPageAuthorAndEditor_NoRevisions — a legacy page with no revision trail
// yields blanks, not an error (the byline simply omits).
func TestPageAuthorAndEditor_NoRevisions(t *testing.T) {
	d := newAPITestDB(t)
	ctx := context.Background()

	alice := seedUser(t, d, "alice", "alicepw123", false)
	spaceID := seedSpace(t, d, "Docs", "docs", alice)
	pageID := seedPage(t, d, spaceID, "Guide")

	author, editor := pageAuthorAndEditor(ctx, d, pageID)
	if author != "" || editor != "" {
		t.Fatalf("author=%q editor=%q want empty/empty", author, editor)
	}
}
