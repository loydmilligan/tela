package mdimport

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/testdb"
)

// newImportTestDB provisions a fresh migrated Postgres database and seeds the
// minimal users + spaces + space_members rows needed for FK constraints on the
// import path (page_revisions.author_id FK → users.id).
func newImportTestDB(t *testing.T) (*sql.DB, int64, int64) {
	t.Helper()
	d := testdb.New(t)
	var userID int64
	if err := d.QueryRow(`INSERT INTO users (username, password_hash, is_instance_admin, is_active)
	                    VALUES ('importer', 'x', 1, 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var spaceID int64
	if err := d.QueryRow(`INSERT INTO spaces (name, slug) VALUES ('Test', 'test') RETURNING id`).Scan(&spaceID); err != nil {
		t.Fatalf("seed space: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO space_members (space_id, user_id, role)
	                     VALUES ($1, $2, 'owner')`, spaceID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return d, spaceID, userID
}

func runImport(t *testing.T, d *sql.DB, spaceID, userID int64, parentID *int64, files []ImportFile, dryRun bool) *Result {
	t.Helper()
	tx, err := d.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	res, err := Import(context.Background(), tx, spaceID, parentID, userID, files, dryRun)
	if err != nil {
		tx.Rollback()
		t.Fatalf("import: %v", err)
	}
	if dryRun {
		tx.Rollback()
	} else {
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}
	return res
}

func countPages(t *testing.T, d *sql.DB, spaceID int64) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM pages WHERE space_id = $1`, spaceID).Scan(&n); err != nil {
		t.Fatalf("count pages: %v", err)
	}
	return n
}

// 1. Single .md at root → 1 page, title from filename.
func TestImport_SingleRootFile_TitleFromFilename(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "hello.md", Content: []byte("just a paragraph, no heading\n")},
	}, false)
	if r.Summary.Created != 1 || len(r.Pages) != 1 {
		t.Fatalf("summary=%+v pages=%d", r.Summary, len(r.Pages))
	}
	if r.Pages[0].Title != "hello" {
		t.Fatalf("title=%q want 'hello'", r.Pages[0].Title)
	}
	if r.Pages[0].ParentID != nil {
		t.Fatalf("parent_id=%v want nil", r.Pages[0].ParentID)
	}
	if countPages(t, d, sp) != 1 {
		t.Fatalf("db has %d pages, want 1", countPages(t, d, sp))
	}
}

// 2. Single dir w/ README.md → 1 index page, README content as body, title = dir name.
func TestImport_SingleDir_WithReadme(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	readme := "# Some Heading\n\nReadme body content.\n"
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "Foo/README.md", Content: []byte(readme)},
	}, false)
	if r.Summary.Created != 1 || len(r.Pages) != 1 {
		t.Fatalf("summary=%+v pages=%d", r.Summary, len(r.Pages))
	}
	if r.Pages[0].Title != "Foo" {
		t.Fatalf("title=%q want 'Foo' (dir name overrides README H1)", r.Pages[0].Title)
	}
	var body string
	if err := d.QueryRow(`SELECT body FROM pages WHERE id = $1`, r.Pages[0].ID).Scan(&body); err != nil {
		t.Fatalf("query body: %v", err)
	}
	if body != readme {
		t.Fatalf("body=%q want full README content", body)
	}
}

// 2b. README has frontmatter title AND H1; directory basename still wins
// (locks Q39 #2 rule: index pages use dir name, not frontmatter / H1).
func TestImport_SingleDir_WithReadme_FrontmatterIgnored(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	readme := "---\ntitle: Should Be Ignored\n---\n# H1 Also Ignored\nbody text\n"
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "Foo/README.md", Content: []byte(readme)},
	}, false)
	if r.Summary.Created != 1 || len(r.Pages) != 1 {
		t.Fatalf("summary=%+v pages=%d", r.Summary, len(r.Pages))
	}
	if r.Pages[0].Title != "Foo" {
		t.Fatalf("title=%q want 'Foo' (dir basename overrides frontmatter title AND H1)", r.Pages[0].Title)
	}
	var body string
	if err := d.QueryRow(`SELECT body FROM pages WHERE id = $1`, r.Pages[0].ID).Scan(&body); err != nil {
		t.Fatalf("query body: %v", err)
	}
	if strings.Contains(body, "title: Should Be Ignored") {
		t.Fatalf("frontmatter leaked into body: %q", body)
	}
	if !strings.Contains(body, "# H1 Also Ignored") {
		t.Fatalf("H1 missing from body after frontmatter strip: %q", body)
	}
	if !strings.Contains(body, "body text") {
		t.Fatalf("body text missing: %q", body)
	}
}

// 3. Single top-level dir, no README, parent_id == space root → flatten:
// strip the dir prefix, no wrapper, files become siblings at the space root.
// Locks Q40 C + Q42 B (no-README half).
func TestImport_FlattenSpaceRoot_NoReadme(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "Notes/alpha.md", Content: []byte("alpha body")},
		{Path: "Notes/beta.md", Content: []byte("beta body")},
	}, false)
	if r.Summary.Created != 2 {
		t.Fatalf("created=%d want 2 (flat, no synthesized 'Notes' wrapper)", r.Summary.Created)
	}
	for _, p := range r.Pages {
		if p.Title == "Notes" {
			t.Fatalf("unexpected wrapper page 'Notes' present after flatten: %+v", p)
		}
		if p.ParentID != nil {
			t.Fatalf("page %q parent_id=%v want nil (flattened to space root)", p.Title, p.ParentID)
		}
	}
	titles := map[string]bool{}
	for _, p := range r.Pages {
		titles[p.Title] = true
	}
	if !titles["alpha"] || !titles["beta"] {
		t.Fatalf("titles=%v want both 'alpha' and 'beta' at root", titles)
	}
}

// 4. Nested dirs (3-deep), README at each level.
func TestImport_NestedDirs_StackedReadmeIndices(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "A/README.md", Content: []byte("A readme")},
		{Path: "A/B/README.md", Content: []byte("AB readme")},
		{Path: "A/B/C/README.md", Content: []byte("ABC readme")},
		{Path: "A/B/C/leaf.md", Content: []byte("leaf body")},
	}, false)
	if r.Summary.Created != 4 {
		t.Fatalf("created=%d want 4", r.Summary.Created)
	}
	titles := map[string]int64{}
	parents := map[int64]*int64{}
	for _, p := range r.Pages {
		titles[p.Title] = p.ID
		parents[p.ID] = p.ParentID
	}
	if parents[titles["A"]] != nil {
		t.Fatalf("A parent=%v want nil", parents[titles["A"]])
	}
	if parents[titles["B"]] == nil || *parents[titles["B"]] != titles["A"] {
		t.Fatalf("B parent=%v want A(%d)", parents[titles["B"]], titles["A"])
	}
	if parents[titles["C"]] == nil || *parents[titles["C"]] != titles["B"] {
		t.Fatalf("C parent=%v want B(%d)", parents[titles["C"]], titles["B"])
	}
	if parents[titles["leaf"]] == nil || *parents[titles["leaf"]] != titles["C"] {
		t.Fatalf("leaf parent=%v want C(%d)", parents[titles["leaf"]], titles["C"])
	}
}

// 5. Title conflict: against existing DB sibling + against in-batch sibling.
func TestImport_TitleConflict_AppendsNumber(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	// Pre-seed an existing top-level "Foo" page.
	if _, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                     VALUES ($1, NULL, 'Foo', 'existing', 0)`, sp); err != nil {
		t.Fatalf("seed existing: %v", err)
	}
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "Foo.md", Content: []byte("first foo")},
		{Path: "Foo.txt-but-md.md", Content: []byte("---\ntitle: Foo\n---\nsecond foo")},
	}, false)
	if r.Summary.ConflictsRenamed != 2 {
		t.Fatalf("conflicts_renamed=%d want 2", r.Summary.ConflictsRenamed)
	}
	titles := []string{}
	for _, p := range r.Pages {
		titles = append(titles, p.Title)
	}
	wantTitles := map[string]bool{"Foo (2)": false, "Foo (3)": false}
	for _, ti := range titles {
		if _, ok := wantTitles[ti]; ok {
			wantTitles[ti] = true
		}
	}
	for ti, seen := range wantTitles {
		if !seen {
			t.Fatalf("missing rename %q in titles=%v", ti, titles)
		}
	}
}

// 6. Frontmatter title overrides filename and H1.
func TestImport_FrontmatterTitle_OverridesFilenameAndH1(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	content := "---\ntitle: Custom\nfoo: bar\n---\n# Different Heading\n\nBody.\n"
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "random.md", Content: []byte(content)},
	}, false)
	if r.Pages[0].Title != "Custom" {
		t.Fatalf("title=%q want 'Custom'", r.Pages[0].Title)
	}
	var body string
	if err := d.QueryRow(`SELECT body FROM pages WHERE id = $1`, r.Pages[0].ID).Scan(&body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if strings.Contains(body, "title: Custom") {
		t.Fatalf("frontmatter not stripped from body: %q", body)
	}
	if !strings.Contains(body, "# Different Heading") {
		t.Fatalf("H1 missing from body after strip: %q", body)
	}
}

// 6b. H1 fallback when frontmatter is absent.
func TestImport_H1FallbackTitle(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "random.md", Content: []byte("# Hello World\n\nBody text.\n")},
	}, false)
	if r.Pages[0].Title != "Hello World" {
		t.Fatalf("title=%q want 'Hello World'", r.Pages[0].Title)
	}
}

// 7. Non-md file → skipped, no page created.
func TestImport_NonMd_Skipped(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "Foo/img.png", Content: []byte{0x89, 0x50, 0x4e, 0x47}},
		{Path: "Foo/README.md", Content: []byte("real content")},
	}, false)
	if r.Summary.Created != 1 || len(r.Skipped) != 1 {
		t.Fatalf("created=%d skipped=%d want 1/1", r.Summary.Created, len(r.Skipped))
	}
	if r.Skipped[0].Reason != "not_markdown" {
		t.Fatalf("skipped reason=%q want 'not_markdown'", r.Skipped[0].Reason)
	}
	if r.Summary.Skipped != 1 {
		t.Fatalf("summary.skipped=%d want 1", r.Summary.Skipped)
	}
}

// 8. dry_run=true → response shows planned pages with negative IDs, DB unchanged.
func TestImport_DryRun_NoDBWrites(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "Foo/README.md", Content: []byte("body")},
		{Path: "Foo/leaf.md", Content: []byte("leaf")},
	}, true)
	if r.Summary.Created != 2 || len(r.Pages) != 2 {
		t.Fatalf("dry-run summary=%+v len(Pages)=%d", r.Summary, len(r.Pages))
	}
	for _, p := range r.Pages {
		if p.ID >= 0 {
			t.Fatalf("dry-run page id=%d, want negative placeholder", p.ID)
		}
	}
	if got := countPages(t, d, sp); got != 0 {
		t.Fatalf("db has %d pages after dry-run, want 0", got)
	}
}

// 10. README case-insensitive: readme.md, Readme.md, README.md.
func TestImport_ReadmeCaseInsensitive(t *testing.T) {
	for _, name := range []string{"readme.md", "Readme.md", "README.md"} {
		t.Run(name, func(t *testing.T) {
			d, sp, u := newImportTestDB(t)
			r := runImport(t, d, sp, u, nil, []ImportFile{
				{Path: "Bar/" + name, Content: []byte("body for " + name)},
			}, false)
			if r.Summary.Created != 1 || r.Pages[0].Title != "Bar" {
				t.Fatalf("for %s: summary=%+v title=%q want 1 created + 'Bar'", name, r.Summary, r.Pages[0].Title)
			}
			var body string
			if err := d.QueryRow(`SELECT body FROM pages WHERE id = $1`, r.Pages[0].ID).Scan(&body); err != nil {
				t.Fatalf("body: %v", err)
			}
			if !strings.Contains(body, "body for "+name) {
				t.Fatalf("for %s: body=%q does not contain README content", name, body)
			}
		})
	}
}

// Path-traversal rejection: `..` segments end up in the errors array.
func TestImport_PathTraversal_Rejected(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "../etc/passwd.md", Content: []byte("evil")},
		{Path: "/abs/path.md", Content: []byte("also evil")},
		{Path: "ok.md", Content: []byte("legit")},
	}, false)
	if r.Summary.Created != 1 || len(r.Errors) != 2 {
		t.Fatalf("created=%d errors=%d want 1/2", r.Summary.Created, len(r.Errors))
	}
}

// Page-revision seeding: every created page should produce one revision row
// with source='import', so the new page already has page-history visible.
// Fixture uses 2 top-level dirs so the flatten pre-pass is a no-op and both
// wrapper-index pages plus the leaf get materialized (3 revs).
func TestImport_SeedsPageRevision(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "Notes/leaf.md", Content: []byte("body")},
		{Path: "Other/leaf.md", Content: []byte("other body")},
	}, false)
	if r.Summary.Created != 4 {
		t.Fatalf("created=%d want 4 (Notes + leaf + Other + leaf)", r.Summary.Created)
	}
	var nRevs int
	if err := d.QueryRow(`SELECT COUNT(*) FROM page_revisions WHERE source = 'import'`).Scan(&nRevs); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if nRevs != 4 {
		t.Fatalf("page_revisions for import=%d want 4", nRevs)
	}
}

// FlattenSpaceRoot_WithReadme_Wrapped — single top-level dir + root README,
// parent_id == space root → wrapper page (title = dir basename, body = README)
// at space root; non-README files nest under wrapper. Locks Q42 B at root.
func TestImport_FlattenSpaceRoot_WithReadme_Wrapped(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "Notes/README.md", Content: []byte("notes overview body")},
		{Path: "Notes/alpha.md", Content: []byte("alpha body")},
		{Path: "Notes/beta.md", Content: []byte("beta body")},
	}, false)
	if r.Summary.Created != 3 {
		t.Fatalf("created=%d want 3 (Notes wrapper + alpha + beta)", r.Summary.Created)
	}
	var wrapper *ImportedPage
	for i := range r.Pages {
		if r.Pages[i].Title == "Notes" {
			wrapper = &r.Pages[i]
			break
		}
	}
	if wrapper == nil {
		t.Fatalf("missing 'Notes' wrapper page in %+v", r.Pages)
	}
	if wrapper.ParentID != nil {
		t.Fatalf("wrapper parent_id=%v want nil (space root)", wrapper.ParentID)
	}
	var body string
	if err := d.QueryRow(`SELECT body FROM pages WHERE id = $1`, wrapper.ID).Scan(&body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body != "notes overview body" {
		t.Fatalf("wrapper body=%q want 'notes overview body'", body)
	}
	for _, p := range r.Pages {
		if p.Title == "Notes" {
			continue
		}
		if p.ParentID == nil || *p.ParentID != wrapper.ID {
			t.Fatalf("child %q parent=%v want %d (wrapper)", p.Title, p.ParentID, wrapper.ID)
		}
	}
}

// FlattenRealParent_NoReadme — single top-level dir, no README, parent_id ==
// a real page → flat siblings under that real parent (no wrapper).
func TestImport_FlattenRealParent_NoReadme(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	// Pre-create a real parent page.
	var parentID int64
	if err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES ($1, NULL, 'Manual', '', 0) RETURNING id`, sp).Scan(&parentID); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	r := runImport(t, d, sp, u, &parentID, []ImportFile{
		{Path: "Notes/alpha.md", Content: []byte("alpha body")},
		{Path: "Notes/beta.md", Content: []byte("beta body")},
	}, false)
	if r.Summary.Created != 2 {
		t.Fatalf("created=%d want 2 (alpha + beta, no wrapper)", r.Summary.Created)
	}
	for _, p := range r.Pages {
		if p.Title == "Notes" {
			t.Fatalf("unexpected wrapper 'Notes' under real parent: %+v", p)
		}
		if p.ParentID == nil || *p.ParentID != parentID {
			t.Fatalf("page %q parent_id=%v want %d (real parent)", p.Title, p.ParentID, parentID)
		}
	}
}

// FlattenRealParent_WithReadme_Wrapped — single top-level dir + root README,
// parent_id == a real page → wrapper nested under real parent, rest under
// wrapper. This is the Q42 B "wrap regardless of parent type" lock.
func TestImport_FlattenRealParent_WithReadme_Wrapped(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	var parentID int64
	if err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES ($1, NULL, 'Manual', '', 0) RETURNING id`, sp).Scan(&parentID); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	r := runImport(t, d, sp, u, &parentID, []ImportFile{
		{Path: "Notes/README.md", Content: []byte("notes overview body")},
		{Path: "Notes/leaf.md", Content: []byte("leaf body")},
	}, false)
	if r.Summary.Created != 2 {
		t.Fatalf("created=%d want 2 (Notes wrapper + leaf)", r.Summary.Created)
	}
	var wrapper *ImportedPage
	for i := range r.Pages {
		if r.Pages[i].Title == "Notes" {
			wrapper = &r.Pages[i]
			break
		}
	}
	if wrapper == nil {
		t.Fatalf("missing 'Notes' wrapper page in %+v", r.Pages)
	}
	if wrapper.ParentID == nil || *wrapper.ParentID != parentID {
		t.Fatalf("wrapper parent_id=%v want %d (real parent)", wrapper.ParentID, parentID)
	}
	var body string
	if err := d.QueryRow(`SELECT body FROM pages WHERE id = $1`, wrapper.ID).Scan(&body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body != "notes overview body" {
		t.Fatalf("wrapper body=%q want 'notes overview body'", body)
	}
	for _, p := range r.Pages {
		if p.Title == "Notes" {
			continue
		}
		if p.ParentID == nil || *p.ParentID != wrapper.ID {
			t.Fatalf("child %q parent=%v want %d (wrapper)", p.Title, p.ParentID, wrapper.ID)
		}
	}
}

// MultipleTopLevelDirs_NoFlatten — 2+ top-level dirs means the pre-pass is
// a no-op and the existing dir→index synthesis fires. Regression lock so
// the flatten path doesn't accidentally swallow this case.
func TestImport_MultipleTopLevelDirs_NoFlatten(t *testing.T) {
	d, sp, u := newImportTestDB(t)
	r := runImport(t, d, sp, u, nil, []ImportFile{
		{Path: "Alpha/a.md", Content: []byte("a body")},
		{Path: "Beta/b.md", Content: []byte("b body")},
	}, false)
	if r.Summary.Created != 4 {
		t.Fatalf("created=%d want 4 (Alpha + a + Beta + b)", r.Summary.Created)
	}
	titles := map[string]*ImportedPage{}
	for i := range r.Pages {
		titles[r.Pages[i].Title] = &r.Pages[i]
	}
	for _, want := range []string{"Alpha", "Beta", "a", "b"} {
		if titles[want] == nil {
			t.Fatalf("missing page %q in %+v", want, r.Pages)
		}
	}
	if titles["Alpha"].ParentID != nil {
		t.Fatalf("Alpha parent_id=%v want nil", titles["Alpha"].ParentID)
	}
	if titles["Beta"].ParentID != nil {
		t.Fatalf("Beta parent_id=%v want nil", titles["Beta"].ParentID)
	}
	if titles["a"].ParentID == nil || *titles["a"].ParentID != titles["Alpha"].ID {
		t.Fatalf("a parent=%v want Alpha(%d)", titles["a"].ParentID, titles["Alpha"].ID)
	}
	if titles["b"].ParentID == nil || *titles["b"].ParentID != titles["Beta"].ID {
		t.Fatalf("b parent=%v want Beta(%d)", titles["b"].ParentID, titles["Beta"].ID)
	}
}
