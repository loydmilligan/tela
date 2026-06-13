package rag

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/zcag/tela/backend/internal/testdb"
)

// seedRagFileUnder attaches a file to a parent page (vs seedRagFile's root file),
// so the parent-page citation path is exercised. Returns the file id.
func seedRagFileUnder(t *testing.T, d *sql.DB, spaceID, parentPageID int64, name, mime string, data []byte) int64 {
	t.Helper()
	var id int64
	if err := d.QueryRow(`INSERT INTO space_files
		(space_id, parent_page_id, name, content_hash, mime, data, byte_size)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		spaceID, parentPageID, name, name+"hash", mime, data, len(data)).Scan(&id); err != nil {
		t.Fatalf("seed file under page: %v", err)
	}
	return id
}

// TestSearch_FileChunks_UnionAndScope proves file chunks join the search pool
// alongside pages, cite the file (not a phantom page), and stay ACL-gated through
// the live space_files row — the anti-leak invariant for the file half.
func TestSearch_FileChunks_UnionAndScope(t *testing.T) {
	d := testdb.New(t)
	ctx := context.Background()

	alice := newUser(t, d, "alice")
	bob := newUser(t, d, "bob")
	s1 := newSpace(t, d, "alpha", alice)
	s2 := newSpace(t, d, "bravo", bob) // alice is NOT a member of s2

	// A page in s1 (so a query can return BOTH a page and a file hit).
	parent := newPage(t, d, s1, "Vendor Notes", "## Contract\nThe vendor MSA covers support terms.")
	// A file attached under that page, in s1.
	fileBody := []byte("# Master Service Agreement\n\n" +
		"This MSA defines the indemnification clause and the liability cap of $2,000,000. " +
		"The quick brown fox jumps over the lazy dog.")
	fileID := seedRagFileUnder(t, d, s1, parent, "msa.md", "text/markdown", fileBody)
	// A secret file in bob's space alice must never retrieve.
	secretFile := seedRagFile(t, d, s2, "secret.md", "text/markdown",
		[]byte("# Secret\n\nThe confidential indemnification liability cap for next quarter."))

	emb := &fakeEmbedder{}
	svc := NewServiceWithEmbedder(d, emb)
	if _, _, err := svc.ReindexSpace(ctx, s1); err != nil {
		t.Fatalf("reindex s1 pages: %v", err)
	}
	if _, err := svc.ReindexFile(ctx, fileID); err != nil {
		t.Fatalf("reindex file: %v", err)
	}
	if _, err := svc.ReindexFile(ctx, secretFile); err != nil {
		t.Fatalf("reindex secret file: %v", err)
	}

	hits, err := svc.Search(ctx, alice, "indemnification liability cap", nil, 20, "hybrid")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var fileHit *Hit
	for i := range hits {
		h := hits[i]
		if h.SpaceID != s1 {
			t.Errorf("hit from unexpected space %d", h.SpaceID)
		}
		if h.SourceKind == "file" {
			if IsFileChunk(h.ChunkID) == false {
				t.Errorf("file hit chunk id %d below file id base", h.ChunkID)
			}
			fileHit = &hits[i]
		}
	}
	if fileHit == nil {
		t.Fatalf("file chunk did not surface in search; hits=%+v", hits)
	}
	if fileHit.FileID != fileID || fileHit.FileName != "msa.md" || fileHit.Title != "msa.md" {
		t.Errorf("file hit citation wrong: %+v", *fileHit)
	}
	if fileHit.PageID != parent {
		t.Errorf("file hit parent page = %d, want %d", fileHit.PageID, parent)
	}
	if fileHit.Hash == "" || fileHit.UpdatedAt == "" {
		t.Errorf("file hit missing hash/updated_at (download + freshness): %+v", *fileHit)
	}

	// ACL: alice must never retrieve bob's file chunk.
	for _, h := range hits {
		if h.SourceKind == "file" && h.FileID == secretFile {
			t.Fatalf("LEAK: alice retrieved bob's file %d", secretFile)
		}
	}
	// Bob CAN find his own file.
	bobHits, err := svc.Search(ctx, bob, "confidential indemnification", nil, 20, "hybrid")
	if err != nil {
		t.Fatalf("bob search: %v", err)
	}
	foundSecret := false
	for _, h := range bobHits {
		if h.FileID == secretFile {
			foundSecret = true
		}
	}
	if !foundSecret {
		t.Error("bob should retrieve his own file")
	}
}

// TestReadChunk_FileRoutingAndScope verifies read_chunk routes a file chunk id to
// file_chunks, returns the full text + file citation, and denies cross-space reads
// indistinguishably from a missing chunk.
func TestReadChunk_FileRoutingAndScope(t *testing.T) {
	d := testdb.New(t)
	ctx := context.Background()
	alice := newUser(t, d, "alice")
	bob := newUser(t, d, "bob")
	s1 := newSpace(t, d, "alpha", alice)
	s2 := newSpace(t, d, "bravo", bob)

	fileID := seedRagFile(t, d, s1, "runbook.md", "text/markdown",
		[]byte("# Deploy\n\nRun make deploy to ship the release to production."))
	secret := seedRagFile(t, d, s2, "secret.md", "text/markdown",
		[]byte("# Plans\n\nThe secret deployment roadmap for the quarter."))

	svc := NewServiceWithEmbedder(d, &fakeEmbedder{})
	if _, err := svc.ReindexFile(ctx, fileID); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if _, err := svc.ReindexFile(ctx, secret); err != nil {
		t.Fatalf("reindex secret: %v", err)
	}

	hits, err := svc.Search(ctx, alice, "deploy release production", nil, 10, "hybrid")
	if err != nil || len(hits) == 0 {
		t.Fatalf("search: %v (hits=%d)", err, len(hits))
	}
	got, err := svc.ReadChunk(ctx, alice, hits[0].ChunkID, nil)
	if err != nil {
		t.Fatalf("read own file chunk: %v", err)
	}
	if got.SourceKind != "file" || got.FileID != fileID || got.Content == "" {
		t.Errorf("file chunk read mismatch: %+v", got)
	}
	if got.Title != "runbook.md" || got.SpaceID != s1 {
		t.Errorf("file chunk citation wrong: %+v", got)
	}

	// Find bob's file chunk id (as bob), confirm alice CANNOT read it.
	bobHits, err := svc.Search(ctx, bob, "secret deployment roadmap", nil, 10, "hybrid")
	if err != nil || len(bobHits) == 0 {
		t.Fatalf("bob search: %v", err)
	}
	if _, err := svc.ReadChunk(ctx, alice, bobHits[0].ChunkID, nil); !errors.Is(err, ErrChunkNotFound) {
		t.Fatalf("LEAK: alice read bob's file chunk %d (err=%v)", bobHits[0].ChunkID, err)
	}
	// A missing file chunk id (in range) → not found, not a different error.
	if _, err := svc.ReadChunk(ctx, alice, fileChunkIDBase+999999, nil); !errors.Is(err, ErrChunkNotFound) {
		t.Errorf("missing file chunk: err=%v, want ErrChunkNotFound", err)
	}
}
