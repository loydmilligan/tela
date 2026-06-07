package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"
)

// dav_space_files_test.go exercises non-markdown files over the WebDAV sync
// surface (migration 0015): arbitrary files round-trip as space_files instead of
// being silently dropped, nest under folder pages, no-op on re-PUT, delete
// softly, and obey the size cap — while OS/editor junk is still swallowed.

func countLiveFiles(t *testing.T, d *sql.DB, spaceID int64) int {
	t.Helper()
	var n int
	if err := d.QueryRowContext(context.Background(),
		`SELECT count(*) FROM space_files WHERE space_id = $1 AND deleted_at IS NULL`, spaceID).Scan(&n); err != nil {
		t.Fatalf("count files: %v", err)
	}
	return n
}

func fileRow(t *testing.T, d *sql.DB, spaceID int64, name string) (parent sql.NullInt64, hash string, ok bool) {
	t.Helper()
	err := d.QueryRowContext(context.Background(),
		`SELECT parent_page_id, content_hash FROM space_files
		   WHERE space_id = $1 AND name = $2 AND deleted_at IS NULL`, spaceID, name).Scan(&parent, &hash)
	if err == sql.ErrNoRows {
		return sql.NullInt64{}, "", false
	}
	if err != nil {
		t.Fatalf("file row %q: %v", name, err)
	}
	return parent, hash, true
}

func TestDAV_NonMarkdownFileRoundTrips(t *testing.T) {
	ts, d, spaceID, folder, token := davFixture(t)

	body := "%PDF-1.4\nbinary-ish payload\n"
	resp, _ := davDo(t, ts, token, "PUT", "/dav/"+folder+"/report.pdf", body, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT file status = %d, want 201", resp.StatusCode)
	}
	// Stored as a file, NOT a page.
	if n := countLiveFiles(t, d, spaceID); n != 1 {
		t.Fatalf("after PUT: %d live files, want 1", n)
	}
	if n := countLivePages(t, d, spaceID); n != 0 {
		t.Fatalf("after PUT: %d live pages, want 0 (a file must not mint a page)", n)
	}
	parent, _, ok := fileRow(t, d, spaceID, "report.pdf")
	if !ok {
		t.Fatal("report.pdf not stored")
	}
	if parent.Valid {
		t.Fatalf("root file parent = %d, want NULL", parent.Int64)
	}

	// GET returns the exact bytes (no markdown transform).
	resp, got := davDo(t, ts, token, "GET", "/dav/"+folder+"/report.pdf", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp.StatusCode)
	}
	if got != body {
		t.Fatalf("GET body = %q, want %q", got, body)
	}

	// PROPFIND lists it alongside pages.
	resp, multi := davDo(t, ts, token, "PROPFIND", "/dav/"+folder+"/", "", map[string]string{"Depth": "1"})
	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("PROPFIND status = %d, want 207", resp.StatusCode)
	}
	if !strings.Contains(multi, "report.pdf") {
		t.Fatalf("PROPFIND missing report.pdf:\n%s", multi)
	}
}

func TestDAV_NonMarkdownNoOpAndOverwrite(t *testing.T) {
	ts, d, spaceID, folder, token := davFixture(t)

	davDo(t, ts, token, "PUT", "/dav/"+folder+"/data.bin", "one", nil)
	_, h1, _ := fileRow(t, d, spaceID, "data.bin")

	// Re-PUT identical bytes → idempotent no-op (no churn, no second row).
	davDo(t, ts, token, "PUT", "/dav/"+folder+"/data.bin", "one", nil)
	if n := countLiveFiles(t, d, spaceID); n != 1 {
		t.Fatalf("after re-PUT: %d files, want 1", n)
	}
	_, h2, _ := fileRow(t, d, spaceID, "data.bin")
	if h1 != h2 {
		t.Fatalf("hash changed on no-op re-PUT: %s → %s", h1, h2)
	}

	// PUT new bytes → content replaced in place (same row, new hash).
	davDo(t, ts, token, "PUT", "/dav/"+folder+"/data.bin", "two", nil)
	if n := countLiveFiles(t, d, spaceID); n != 1 {
		t.Fatalf("after overwrite: %d files, want 1 (replace in place)", n)
	}
	_, h3, _ := fileRow(t, d, spaceID, "data.bin")
	if h3 == h1 {
		t.Fatal("hash unchanged after overwrite with different bytes")
	}
	if _, got := davDo(t, ts, token, "GET", "/dav/"+folder+"/data.bin", "", nil); got != "two" {
		t.Fatalf("GET after overwrite = %q, want %q", got, "two")
	}
}

func TestDAV_NonMarkdownNestsUnderFolderPage(t *testing.T) {
	ts, d, spaceID, folder, token := davFixture(t)

	if resp, _ := davDo(t, ts, token, "MKCOL", "/dav/"+folder+"/assets", "", nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("MKCOL assets status = %d, want 201", resp.StatusCode)
	}
	if resp, _ := davDo(t, ts, token, "PUT", "/dav/"+folder+"/assets/logo.png", "PNGDATA", nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT nested file status = %d, want 201", resp.StatusCode)
	}
	parent, _, ok := fileRow(t, d, spaceID, "logo.png")
	if !ok || !parent.Valid {
		t.Fatalf("logo.png not nested under a page (ok=%v valid=%v)", ok, parent.Valid)
	}
	if p, found := pageByTitle(t, d, spaceID, "assets"); !found || parent.Int64 != p.ID {
		t.Fatalf("logo.png parent = %d, want assets page id", parent.Int64)
	}

	// Listing the folder page surfaces the nested file.
	resp, multi := davDo(t, ts, token, "PROPFIND", "/dav/"+folder+"/assets/", "", map[string]string{"Depth": "1"})
	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("PROPFIND status = %d, want 207", resp.StatusCode)
	}
	if !strings.Contains(multi, "logo.png") {
		t.Fatalf("folder PROPFIND missing logo.png:\n%s", multi)
	}
}

func TestDAV_NonMarkdownDeleteIsSoftAndGone(t *testing.T) {
	ts, d, spaceID, folder, token := davFixture(t)

	davDo(t, ts, token, "PUT", "/dav/"+folder+"/temp.bin", "x", nil)
	if resp, _ := davDo(t, ts, token, "DELETE", "/dav/"+folder+"/temp.bin", "", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", resp.StatusCode)
	}
	if n := countLiveFiles(t, d, spaceID); n != 0 {
		t.Fatalf("after DELETE: %d live files, want 0", n)
	}
	// Soft — the row survives for recovery.
	var deleted int
	d.QueryRowContext(context.Background(),
		`SELECT count(*) FROM space_files WHERE space_id = $1 AND deleted_at IS NOT NULL`, spaceID).Scan(&deleted)
	if deleted != 1 {
		t.Fatalf("soft-deleted rows = %d, want 1", deleted)
	}
	if resp, _ := davDo(t, ts, token, "GET", "/dav/"+folder+"/temp.bin", "", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after delete = %d, want 404", resp.StatusCode)
	}
}

func TestDAV_NonMarkdownMoveRenames(t *testing.T) {
	ts, d, spaceID, folder, token := davFixture(t)

	davDo(t, ts, token, "PUT", "/dav/"+folder+"/old.bin", "payload", nil)
	dest := ts.URL + "/dav/" + folder + "/new.bin"
	resp, _ := davDo(t, ts, token, "MOVE", "/dav/"+folder+"/old.bin", "", map[string]string{"Destination": dest})
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("MOVE status = %d, want 201/204", resp.StatusCode)
	}
	if _, _, ok := fileRow(t, d, spaceID, "new.bin"); !ok {
		t.Fatal("new.bin not found after MOVE")
	}
	if _, _, ok := fileRow(t, d, spaceID, "old.bin"); ok {
		t.Fatal("old.bin still present after MOVE")
	}
	if _, got := davDo(t, ts, token, "GET", "/dav/"+folder+"/new.bin", "", nil); got != "payload" {
		t.Fatalf("GET new.bin = %q, want %q", got, "payload")
	}
}

func TestDAV_JunkFilesStillDropped(t *testing.T) {
	ts, d, spaceID, folder, token := davFixture(t)

	for _, junk := range []string{".DS_Store", "._note.md", "draft.swp", "Thumbs.db"} {
		davDo(t, ts, token, "PUT", "/dav/"+folder+"/"+junk, "junk", nil)
	}
	if n := countLiveFiles(t, d, spaceID); n != 0 {
		t.Fatalf("junk created %d files, want 0", n)
	}
	if n := countLivePages(t, d, spaceID); n != 0 {
		t.Fatalf("junk created %d pages, want 0", n)
	}
}

func spaceBySlug(t *testing.T, d *sql.DB, uid int64, slug string) (int64, bool) {
	t.Helper()
	var id int64
	err := d.QueryRowContext(context.Background(), `
		SELECT s.id FROM spaces s
		  JOIN space_members m ON m.space_id = s.id AND m.user_id = $1 AND m.role = 'owner'
		 WHERE s.slug = $2`, uid, slug).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false
	}
	if err != nil {
		t.Fatalf("space by slug %q: %v", slug, err)
	}
	return id, true
}

func TestDAV_MkcolAtRootCreatesSpace(t *testing.T) {
	ts, d := newWiredServer(t)
	uid := seedUser(t, d, "owner", "pw-owner-123", false)
	token, _ := seedAPIKeyForUser(t, d, uid, "write", nil)

	if resp, _ := davDo(t, ts, token, "MKCOL", "/dav/notes", "", nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("MKCOL new space status = %d, want 201", resp.StatusCode)
	}
	spaceID, ok := spaceBySlug(t, d, uid, "notes")
	if !ok {
		t.Fatal("space 'notes' not created / not owned by caller")
	}

	// It's immediately usable over /dav: list it, then write a page into it.
	if resp, _ := davDo(t, ts, token, "PROPFIND", "/dav/notes/", "", map[string]string{"Depth": "1"}); resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("PROPFIND new space status = %d, want 207", resp.StatusCode)
	}
	if resp, _ := davDo(t, ts, token, "PUT", "/dav/notes/hello.md", "hi\n", nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT into new space status = %d, want 201", resp.StatusCode)
	}
	if n := countLivePages(t, d, spaceID); n != 1 {
		t.Fatalf("new space has %d pages, want 1", n)
	}
}

func TestDAV_MkcolSpaceCreationDisabled(t *testing.T) {
	t.Setenv("TELA_WEBDAV_CREATE_SPACES", "0")
	ts, d := newWiredServer(t)
	uid := seedUser(t, d, "owner", "pw-owner-123", false)
	token, _ := seedAPIKeyForUser(t, d, uid, "write", nil)

	if resp, _ := davDo(t, ts, token, "MKCOL", "/dav/nope", "", nil); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("MKCOL with creation disabled = %d, want 405", resp.StatusCode)
	}
	if _, ok := spaceBySlug(t, d, uid, "nope"); ok {
		t.Fatal("space created despite TELA_WEBDAV_CREATE_SPACES=0")
	}
}

func TestDAV_PinnedKeyCannotCreateSpace(t *testing.T) {
	ts, d := newWiredServer(t)
	uid := seedUser(t, d, "owner", "pw-owner-123", false)
	pinned := seedSpace(t, d, "Engineering", "eng", uid)
	token, _ := seedAPIKeyForUser(t, d, uid, "write", &pinned)

	if resp, _ := davDo(t, ts, token, "MKCOL", "/dav/other", "", nil); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("pinned-key MKCOL = %d, want 405", resp.StatusCode)
	}
	if _, ok := spaceBySlug(t, d, uid, "other"); ok {
		t.Fatal("pinned key minted a new space")
	}
}

func TestDAV_FileSizeCapRejects(t *testing.T) {
	t.Setenv("TELA_WEBDAV_FILE_MAX_BYTES", "8")
	ts, d, spaceID, folder, token := davFixture(t)

	resp, _ := davDo(t, ts, token, "PUT", "/dav/"+folder+"/big.bin", "way more than eight bytes", nil)
	if resp.StatusCode < 400 {
		t.Fatalf("oversized PUT status = %d, want a 4xx/5xx failure (not silent drop)", resp.StatusCode)
	}
	if n := countLiveFiles(t, d, spaceID); n != 0 {
		t.Fatalf("oversized PUT stored %d files, want 0", n)
	}
}
