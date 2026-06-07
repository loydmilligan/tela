package api

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/models"
)

// dav_test.go exercises the WebDAV sync surface end-to-end through the wired
// server (auth.Middleware bypass + the real webdav.Handler over davFS), using a
// PAT as the Basic-auth password the way stock clients (rclone, Finder) do.

// davClient is an http.Client that does NOT auto-follow redirects, so a PUT
// that would 301 (e.g. a missing trailing slash) surfaces instead of silently
// turning into a GET.
var davClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func davDo(t *testing.T, ts *httptest.Server, token, method, p, body string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+p, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, p, err)
	}
	if token != "" {
		req.SetBasicAuth("anyuser", token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := davClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, p, err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(data)
}

func countLivePages(t *testing.T, d *sql.DB, spaceID int64) int {
	t.Helper()
	var n int
	if err := d.QueryRowContext(context.Background(),
		`SELECT count(*) FROM pages WHERE space_id = $1 AND deleted_at IS NULL`, spaceID).Scan(&n); err != nil {
		t.Fatalf("count pages: %v", err)
	}
	return n
}

func pageByTitle(t *testing.T, d *sql.DB, spaceID int64, title string) (models.Page, bool) {
	t.Helper()
	row := d.QueryRowContext(context.Background(),
		`SELECT id, space_id, parent_id, title, body, position, props, created_at, updated_at
		   FROM pages WHERE space_id = $1 AND title = $2 AND deleted_at IS NULL`, spaceID, title)
	p, err := scanPageFromRow(row)
	if err == sql.ErrNoRows {
		return models.Page{}, false
	}
	if err != nil {
		t.Fatalf("page by title %q: %v", title, err)
	}
	return p, true
}

// davFixture seeds one owner + one space + a write-scope PAT, returns the wired
// server, db, space id, the space folder name, and the raw token.
func davFixture(t *testing.T) (*httptest.Server, *sql.DB, int64, string, string) {
	t.Helper()
	ts, d := newWiredServer(t)
	uid := seedUser(t, d, "owner", "pw-owner-123", false)
	spaceID := seedSpace(t, d, "Engineering", "eng", uid)
	token, _ := seedAPIKeyForUser(t, d, uid, "write", nil)
	return ts, d, spaceID, "eng", token
}

func TestDAV_AuthRequired(t *testing.T) {
	ts, _, _, folder, _ := davFixture(t)
	resp, _ := davDo(t, ts, "", "PROPFIND", "/dav/"+folder+"/", "", map[string]string{"Depth": "1"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-cred PROPFIND status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, "Basic") {
		t.Fatalf("WWW-Authenticate = %q, want Basic challenge", got)
	}
}

func TestDAV_PutCreatesAndGetRoundTrips(t *testing.T) {
	ts, d, spaceID, folder, token := davFixture(t)

	// No H1 / no frontmatter title → the title (and thus the slug/filename) comes
	// from the filename, so note.md round-trips to /dav/eng/note.md.
	body := "Hello from rclone.\n"
	resp, _ := davDo(t, ts, token, "PUT", "/dav/"+folder+"/note.md", body, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT create status = %d, want 201", resp.StatusCode)
	}
	if countLivePages(t, d, spaceID) != 1 {
		t.Fatalf("after create: %d live pages, want 1", countLivePages(t, d, spaceID))
	}
	p, ok := pageByTitle(t, d, spaceID, "note")
	if !ok {
		t.Fatal("page 'note' not created")
	}
	if p.Body != "Hello from rclone.\n" {
		t.Fatalf("stored body = %q, want %q", p.Body, "Hello from rclone.\n")
	}

	// GET returns canonical markdown: frontmatter (carrying the assigned id) + body.
	resp, got := davDo(t, ts, token, "GET", "/dav/"+folder+"/note.md", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(got, "Hello from rclone.") {
		t.Fatalf("GET body missing content:\n%s", got)
	}
	if !strings.Contains(got, "id:") || !strings.Contains(got, "title: note") {
		t.Fatalf("GET body missing frontmatter id/title:\n%s", got)
	}

	// Re-PUT the exact bytes we just read (the rclone steady state): binds by the
	// frontmatter id, nothing differs → idempotent no-op, NO duplicate page.
	resp, _ = davDo(t, ts, token, "PUT", "/dav/"+folder+"/note.md", got, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("re-PUT status = %d, want 201", resp.StatusCode)
	}
	if n := countLivePages(t, d, spaceID); n != 1 {
		t.Fatalf("after re-PUT: %d live pages, want 1 (no ping-pong duplicate)", n)
	}
}

func TestDAV_PropfindListsTree(t *testing.T) {
	ts, _, _, folder, token := davFixture(t)
	// Build a small tree over WebDAV: a folder page "guide" with one child.
	if resp, _ := davDo(t, ts, token, "MKCOL", "/dav/"+folder+"/guide", "", nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("MKCOL guide status = %d, want 201", resp.StatusCode)
	}
	if resp, _ := davDo(t, ts, token, "PUT", "/dav/"+folder+"/guide/setup.md", "Install.\n", nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT child status = %d, want 201", resp.StatusCode)
	}

	// Depth-1 PROPFIND on the space folder lists the root page as both a file and
	// (because it has a child) a folder.
	resp, multi := davDo(t, ts, token, "PROPFIND", "/dav/"+folder+"/", "", map[string]string{"Depth": "1"})
	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("PROPFIND status = %d, want 207\n%s", resp.StatusCode, multi)
	}
	if !strings.Contains(multi, "guide.md") {
		t.Fatalf("space PROPFIND missing guide.md:\n%s", multi)
	}

	// Depth-1 PROPFIND on the page folder lists its child.
	resp, multi = davDo(t, ts, token, "PROPFIND", "/dav/"+folder+"/guide/", "", map[string]string{"Depth": "1"})
	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("child PROPFIND status = %d, want 207\n%s", resp.StatusCode, multi)
	}
	if !strings.Contains(multi, "setup.md") {
		t.Fatalf("page-folder PROPFIND missing setup.md:\n%s", multi)
	}
}

func TestDAV_MkcolThenIndexPutBindsNoDuplicate(t *testing.T) {
	ts, d, spaceID, folder, token := davFixture(t)

	// rclone-style upload of a folder page: MKCOL the container, PUT a child, then
	// PUT the container's own index file. The index PUT must bind to the MKCOL'd
	// page (path-fallback), not mint a second "guide".
	resp, _ := davDo(t, ts, token, "MKCOL", "/dav/"+folder+"/guide", "", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("MKCOL status = %d, want 201", resp.StatusCode)
	}
	resp, _ = davDo(t, ts, token, "PUT", "/dav/"+folder+"/guide/setup.md", "Install steps.\n", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("child PUT status = %d, want 201", resp.StatusCode)
	}
	resp, _ = davDo(t, ts, token, "PUT", "/dav/"+folder+"/guide.md", "The guide body.\n", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("index PUT status = %d, want 201", resp.StatusCode)
	}

	if n := countLivePages(t, d, spaceID); n != 2 {
		t.Fatalf("got %d live pages, want 2 (guide + setup, no duplicate guide)", n)
	}
	guide, ok := pageByTitle(t, d, spaceID, "guide")
	if !ok {
		t.Fatal("guide page missing")
	}
	if guide.Body != "The guide body.\n" {
		t.Fatalf("guide body = %q, want index PUT to have filled it in", guide.Body)
	}
}

func TestDAV_DeleteSoftDeletes(t *testing.T) {
	ts, d, spaceID, folder, token := davFixture(t)
	davDo(t, ts, token, "PUT", "/dav/"+folder+"/note.md", "doomed\n", nil)
	if countLivePages(t, d, spaceID) != 1 {
		t.Fatal("setup: expected 1 page")
	}

	resp, _ := davDo(t, ts, token, "DELETE", "/dav/"+folder+"/note.md", "", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", resp.StatusCode)
	}
	if n := countLivePages(t, d, spaceID); n != 0 {
		t.Fatalf("after DELETE: %d live pages, want 0", n)
	}
	resp, _ = davDo(t, ts, token, "GET", "/dav/"+folder+"/note.md", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after DELETE status = %d, want 404", resp.StatusCode)
	}
}

func TestDAV_ReadScopeCannotWrite(t *testing.T) {
	ts, d, _, folder, _ := davFixture(t)
	uid := seedUser(t, d, "reader", "pw-reader-123", false)
	// reader is a member of the eng space (so reads resolve) but holds a read PAT.
	var spaceID int64
	d.QueryRowContext(context.Background(), `SELECT id FROM spaces WHERE slug = 'eng'`).Scan(&spaceID)
	seedMember(t, d, spaceID, uid, "viewer")
	roTok, _ := seedAPIKeyForUser(t, d, uid, "read", nil)

	resp, _ := davDo(t, ts, roTok, "PROPFIND", "/dav/"+folder+"/", "", map[string]string{"Depth": "1"})
	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("read-scope PROPFIND status = %d, want 207", resp.StatusCode)
	}
	resp, _ = davDo(t, ts, roTok, "PUT", "/dav/"+folder+"/x.md", "nope\n", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-scope PUT status = %d, want 403", resp.StatusCode)
	}
}

func TestDAV_SpacePinnedPATHidesOtherSpaces(t *testing.T) {
	ts, d, engID, _, _ := davFixture(t)
	// owner also owns a second space; a PAT pinned to eng must not expose it.
	var ownerID int64
	d.QueryRowContext(context.Background(), `SELECT user_id FROM space_members WHERE space_id = $1 AND role='owner'`, engID).Scan(&ownerID)
	otherID := seedSpace(t, d, "Personal", "personal", ownerID)
	_ = otherID
	pinned, _ := seedAPIKeyForUser(t, d, ownerID, "write", &engID)

	resp, multi := davDo(t, ts, pinned, "PROPFIND", "/dav/", "", map[string]string{"Depth": "1"})
	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("root PROPFIND status = %d, want 207", resp.StatusCode)
	}
	if !strings.Contains(multi, "eng") {
		t.Fatalf("pinned root missing its own space:\n%s", multi)
	}
	if strings.Contains(multi, "personal") {
		t.Fatalf("pinned PAT leaked the other space:\n%s", multi)
	}
	// Direct access to the other space is also denied (not in the listing → 404).
	resp, _ = davDo(t, ts, pinned, "PROPFIND", "/dav/personal/", "", map[string]string{"Depth": "1"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("pinned access to other space status = %d, want 404", resp.StatusCode)
	}
}

// --- pure unit tests (no DB) for the path + slug layer ---

func TestDavSplit(t *testing.T) {
	cases := []struct {
		in   string
		segs []string
		ok   bool
	}{
		{"/", nil, true},
		{"", nil, true},
		{"/eng", []string{"eng"}, true},
		{"/eng/Notes.md", []string{"eng", "Notes.md"}, true},
		{"/eng/a/b.md", []string{"eng", "a", "b.md"}, true},
		{"/eng/../secret", nil, false},
		{"/eng//x", nil, false},
		{"/eng/./x", nil, false},
	}
	for _, c := range cases {
		segs, ok := davSplit(c.in)
		if ok != c.ok {
			t.Fatalf("davSplit(%q) ok = %v, want %v", c.in, ok, c.ok)
		}
		if ok && strings.Join(segs, "/") != strings.Join(c.segs, "/") {
			t.Fatalf("davSplit(%q) = %v, want %v", c.in, segs, c.segs)
		}
	}
}

func TestSiblingSlugsDedup(t *testing.T) {
	sibs := []models.Page{
		{ID: 1, Title: "Notes"},
		{ID: 2, Title: "Notes"}, // same slug → -2
		{ID: 3, Title: "Other"},
	}
	got := siblingSlugs(sibs)
	if got[1] != "notes" || got[2] != "notes-2" || got[3] != "other" {
		t.Fatalf("siblingSlugs = %v, want notes/notes-2/other", got)
	}
}

func TestSpaceTreeResolve(t *testing.T) {
	pid := int64(1)
	t0 := &spaceTree{
		children: map[int64][]models.Page{
			rootParentKey: {{ID: 1, Title: "Guide"}},
			1:             {{ID: 2, Title: "Setup", ParentID: &pid}},
		},
		slug: map[int64]string{1: "guide", 2: "setup"},
	}
	// file form
	if p, isFile, ok := t0.resolve([]string{"guide.md"}); !ok || !isFile || p.ID != 1 {
		t.Fatalf("resolve guide.md = (%d,%v,%v), want (1,true,true)", p.ID, isFile, ok)
	}
	// folder form
	if p, isFile, ok := t0.resolve([]string{"guide"}); !ok || isFile || p.ID != 1 {
		t.Fatalf("resolve guide = (%d,%v,%v), want (1,false,true)", p.ID, isFile, ok)
	}
	// nested file
	if p, isFile, ok := t0.resolve([]string{"guide", "setup.md"}); !ok || !isFile || p.ID != 2 {
		t.Fatalf("resolve guide/setup.md = (%d,%v,%v), want (2,true,true)", p.ID, isFile, ok)
	}
	// .md mid-path is malformed
	if _, _, ok := t0.resolve([]string{"guide.md", "setup.md"}); ok {
		t.Fatal("resolve with .md mid-path should fail")
	}
	// unknown
	if _, _, ok := t0.resolve([]string{"missing.md"}); ok {
		t.Fatal("resolve unknown should fail")
	}
}
