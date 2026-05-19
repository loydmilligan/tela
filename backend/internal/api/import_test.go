package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
)

type importFilePart struct {
	relPath string
	body    string
}

// postImport builds a multipart/form-data POST to /api/spaces/{id}/import.
// Each entry in files becomes one `files` part whose filename carries the
// relative path verbatim — mirrors what the FE sends via
// `formData.append('files', file, file.webkitRelativePath)`.
func postImport(t *testing.T, c *http.Client, baseURL string, spaceID int64, parentID *int64, dryRun bool, files []importFilePart) (*http.Response, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if parentID != nil {
		if err := mw.WriteField("parent_id", fmt.Sprintf("%d", *parentID)); err != nil {
			t.Fatalf("write parent_id: %v", err)
		}
	}
	if dryRun {
		if err := mw.WriteField("dry_run", "true"); err != nil {
			t.Fatalf("write dry_run: %v", err)
		}
	}
	for _, f := range files {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition",
			fmt.Sprintf(`form-data; name="files"; filename=%q`, f.relPath))
		h.Set("Content-Type", "application/octet-stream")
		part, err := mw.CreatePart(h)
		if err != nil {
			t.Fatalf("create part: %v", err)
		}
		if _, err := part.Write([]byte(f.body)); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/api/spaces/%d/import", baseURL, spaceID), &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, body
}

type importResponse struct {
	Summary struct {
		Created          int `json:"created"`
		Skipped          int `json:"skipped"`
		ConflictsRenamed int `json:"conflicts_renamed"`
	} `json:"summary"`
	Pages []struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		ParentID *int64 `json:"parent_id"`
		Path     string `json:"path"`
	} `json:"pages"`
	Skipped []struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	} `json:"skipped"`
	Errors []struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	} `json:"errors"`
}

func decodeImportResp(t *testing.T, body []byte) importResponse {
	t.Helper()
	var got importResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode import response: %v (body=%s)", err, body)
	}
	return got
}

// TestImport_FullFlow exercises the markdown-import endpoint end-to-end.
// One fixture covers every published scenario so a regression in any branch
// lights up fast.
func TestImport_FullFlow(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	_ = seedUser(t, d, "eve", "evepw12345", false)
	space := seedSpace(t, d, "Test Space", "test-space", admin)
	seedMember(t, d, space, bob, roleViewer)

	adminC := loginClient(t, ts, "admin", "adminpw12")
	bobC := loginClient(t, ts, "bob", "bobpw1234")
	eveC := loginClient(t, ts, "eve", "evepw12345")

	// 1. Single .md at root → 1 page, title from filename.
	t.Run("single_root_file", func(t *testing.T) {
		resp, body := postImport(t, adminC, ts.URL, space, nil, false, []importFilePart{
			{relPath: "lonely.md", body: "body only, no heading\n"},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", resp.StatusCode, body)
		}
		got := decodeImportResp(t, body)
		if got.Summary.Created != 1 || len(got.Pages) != 1 {
			t.Fatalf("summary=%+v len(Pages)=%d", got.Summary, len(got.Pages))
		}
		if got.Pages[0].Title != "lonely" {
			t.Fatalf("title=%q want 'lonely'", got.Pages[0].Title)
		}
		if got.Pages[0].ParentID != nil {
			t.Fatalf("parent_id=%v want nil", got.Pages[0].ParentID)
		}
	})

	// 2. Single dir w/ README → index page; README content as body; title = dir name.
	t.Run("single_dir_with_readme", func(t *testing.T) {
		resp, body := postImport(t, adminC, ts.URL, space, nil, false, []importFilePart{
			{relPath: "Bar/README.md", body: "# Some H1\n\nBar README body.\n"},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", resp.StatusCode, body)
		}
		got := decodeImportResp(t, body)
		if got.Summary.Created != 1 || got.Pages[0].Title != "Bar" {
			t.Fatalf("summary=%+v title=%q want 1/'Bar'", got.Summary, got.Pages[0].Title)
		}
		var dbBody string
		if err := d.QueryRow(`SELECT body FROM pages WHERE id = ?`, got.Pages[0].ID).Scan(&dbBody); err != nil {
			t.Fatalf("query body: %v", err)
		}
		if !strings.Contains(dbBody, "Bar README body") {
			t.Fatalf("README content missing from body: %q", dbBody)
		}
	})

	// 3. Single top-level dir without README → flatten pre-pass strips the
	// shared prefix; files become siblings at the request's parent_id (here
	// space root). No wrapper page is created. Locks M14.5 Q40 C + Q42 B.
	t.Run("single_dir_no_readme_flattens", func(t *testing.T) {
		resp, body := postImport(t, adminC, ts.URL, space, nil, false, []importFilePart{
			{relPath: "Notes/alpha.md", body: "alpha body"},
			{relPath: "Notes/beta.md", body: "beta body"},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", resp.StatusCode, body)
		}
		got := decodeImportResp(t, body)
		if got.Summary.Created != 2 {
			t.Fatalf("created=%d want 2 (alpha + beta, no synthesized 'Notes')", got.Summary.Created)
		}
		for _, p := range got.Pages {
			if p.Title == "Notes" {
				t.Fatalf("unexpected wrapper 'Notes' after flatten: %+v", p)
			}
			if p.ParentID != nil {
				t.Fatalf("page %q parent_id=%v want nil (flattened to space root)", p.Title, p.ParentID)
			}
		}
	})

	// 4. Nested 3-deep with README at each level.
	t.Run("nested_three_deep", func(t *testing.T) {
		resp, body := postImport(t, adminC, ts.URL, space, nil, false, []importFilePart{
			{relPath: "X/README.md", body: "X readme"},
			{relPath: "X/Y/README.md", body: "XY readme"},
			{relPath: "X/Y/Z/README.md", body: "XYZ readme"},
			{relPath: "X/Y/Z/leaf.md", body: "# Leaf Title\nbody"},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", resp.StatusCode, body)
		}
		got := decodeImportResp(t, body)
		if got.Summary.Created != 4 {
			t.Fatalf("created=%d want 4", got.Summary.Created)
		}
		titles := map[string]int64{}
		parents := map[int64]*int64{}
		for _, p := range got.Pages {
			titles[p.Title] = p.ID
			parents[p.ID] = p.ParentID
		}
		if titles["X"] == 0 || titles["Y"] == 0 || titles["Z"] == 0 || titles["Leaf Title"] == 0 {
			t.Fatalf("missing titles in %+v", titles)
		}
		if parents[titles["Y"]] == nil || *parents[titles["Y"]] != titles["X"] {
			t.Fatalf("Y parent=%v want X(%d)", parents[titles["Y"]], titles["X"])
		}
		if parents[titles["Z"]] == nil || *parents[titles["Z"]] != titles["Y"] {
			t.Fatalf("Z parent=%v want Y(%d)", parents[titles["Z"]], titles["Y"])
		}
		if parents[titles["Leaf Title"]] == nil || *parents[titles["Leaf Title"]] != titles["Z"] {
			t.Fatalf("leaf parent=%v want Z(%d)", parents[titles["Leaf Title"]], titles["Z"])
		}
	})

	// 5a. Title conflict against existing DB sibling at root.
	t.Run("conflict_vs_existing_db_sibling", func(t *testing.T) {
		// Pre-seed a top-level 'Solo' page in the test space.
		if _, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
		                     VALUES (?, NULL, 'Solo', 'pre', 99)`, space); err != nil {
			t.Fatalf("seed existing: %v", err)
		}
		resp, body := postImport(t, adminC, ts.URL, space, nil, false, []importFilePart{
			{relPath: "Solo.md", body: "new solo"},
		})
		got := decodeImportResp(t, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", resp.StatusCode, body)
		}
		if got.Summary.ConflictsRenamed != 1 || got.Pages[0].Title != "Solo (2)" {
			t.Fatalf("summary=%+v title=%q want 1 rename + 'Solo (2)'", got.Summary, got.Pages[0].Title)
		}
	})

	// 5b. Title conflict among in-batch siblings.
	t.Run("conflict_within_batch", func(t *testing.T) {
		resp, body := postImport(t, adminC, ts.URL, space, nil, false, []importFilePart{
			{relPath: "Twin/a.md", body: "---\ntitle: Twin Kid\n---\nfirst"},
			{relPath: "Twin/b.md", body: "---\ntitle: Twin Kid\n---\nsecond"},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", resp.StatusCode, body)
		}
		got := decodeImportResp(t, body)
		titles := []string{}
		for _, p := range got.Pages {
			if p.Title != "Twin" {
				titles = append(titles, p.Title)
			}
		}
		want := map[string]bool{"Twin Kid": false, "Twin Kid (2)": false}
		for _, ti := range titles {
			if _, ok := want[ti]; ok {
				want[ti] = true
			}
		}
		for ti, seen := range want {
			if !seen {
				t.Fatalf("missing %q in batch titles=%v", ti, titles)
			}
		}
		if got.Summary.ConflictsRenamed != 1 {
			t.Fatalf("conflicts_renamed=%d want 1", got.Summary.ConflictsRenamed)
		}
	})

	// 6. Frontmatter title overrides filename + H1.
	t.Run("frontmatter_title_overrides", func(t *testing.T) {
		content := "---\ntitle: Override Wins\n---\n# Wrong\n\nbody"
		resp, body := postImport(t, adminC, ts.URL, space, nil, false, []importFilePart{
			{relPath: "random-name.md", body: content},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", resp.StatusCode, body)
		}
		got := decodeImportResp(t, body)
		if got.Pages[0].Title != "Override Wins" {
			t.Fatalf("title=%q want 'Override Wins'", got.Pages[0].Title)
		}
		var dbBody string
		if err := d.QueryRow(`SELECT body FROM pages WHERE id = ?`, got.Pages[0].ID).Scan(&dbBody); err != nil {
			t.Fatalf("body: %v", err)
		}
		if strings.Contains(dbBody, "title: Override Wins") {
			t.Fatalf("frontmatter leaked into body: %q", dbBody)
		}
	})

	// 7. Non-md file → skipped, no page created.
	t.Run("non_md_skipped", func(t *testing.T) {
		resp, body := postImport(t, adminC, ts.URL, space, nil, false, []importFilePart{
			{relPath: "img.png", body: "fake-png-bytes"},
			{relPath: "real.md", body: "real body"},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", resp.StatusCode, body)
		}
		got := decodeImportResp(t, body)
		if got.Summary.Created != 1 || got.Summary.Skipped != 1 || len(got.Skipped) != 1 {
			t.Fatalf("summary=%+v skipped=%+v want created=1 skipped=1", got.Summary, got.Skipped)
		}
		if got.Skipped[0].Reason != "not_markdown" {
			t.Fatalf("skipped reason=%q want 'not_markdown'", got.Skipped[0].Reason)
		}
	})

	// 8. dry_run=true → no DB writes; placeholders negative.
	t.Run("dry_run", func(t *testing.T) {
		var beforeCount int
		if err := d.QueryRow(`SELECT COUNT(*) FROM pages WHERE space_id = ?`, space).Scan(&beforeCount); err != nil {
			t.Fatalf("count before: %v", err)
		}
		resp, body := postImport(t, adminC, ts.URL, space, nil, true, []importFilePart{
			{relPath: "Plan/README.md", body: "p"},
			{relPath: "Plan/inner.md", body: "i"},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", resp.StatusCode, body)
		}
		got := decodeImportResp(t, body)
		if got.Summary.Created != 2 {
			t.Fatalf("dry_run summary=%+v want created=2", got.Summary)
		}
		for _, p := range got.Pages {
			if p.ID >= 0 {
				t.Fatalf("dry_run page id=%d not negative", p.ID)
			}
		}
		var afterCount int
		if err := d.QueryRow(`SELECT COUNT(*) FROM pages WHERE space_id = ?`, space).Scan(&afterCount); err != nil {
			t.Fatalf("count after: %v", err)
		}
		if afterCount != beforeCount {
			t.Fatalf("dry_run wrote rows: before=%d after=%d", beforeCount, afterCount)
		}
	})

	// 9a. Viewer bob → 403 viewer_no_write.
	t.Run("viewer_forbidden", func(t *testing.T) {
		resp, body := postImport(t, bobC, ts.URL, space, nil, false, []importFilePart{
			{relPath: "x.md", body: "x"},
		})
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"code":"viewer_no_write"`) {
			t.Fatalf("viewer status=%d body=%s want 403 viewer_no_write", resp.StatusCode, body)
		}
	})

	// 9b. Non-member eve → 403 forbidden.
	t.Run("non_member_forbidden", func(t *testing.T) {
		resp, body := postImport(t, eveC, ts.URL, space, nil, false, []importFilePart{
			{relPath: "x.md", body: "x"},
		})
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"code":"forbidden"`) {
			t.Fatalf("non-member status=%d body=%s want 403 forbidden", resp.StatusCode, body)
		}
	})

	// 10. README case-insensitive.
	t.Run("readme_case_insensitive", func(t *testing.T) {
		for _, name := range []string{"readme.md", "Readme.md", "README.md"} {
			subdir := "CaseTest" + strings.ReplaceAll(name, ".", "")
			resp, body := postImport(t, adminC, ts.URL, space, nil, false, []importFilePart{
				{relPath: subdir + "/" + name, body: "body for " + name},
			})
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s status=%d body=%s", name, resp.StatusCode, body)
			}
			got := decodeImportResp(t, body)
			if got.Summary.Created != 1 || got.Pages[0].Title != subdir {
				t.Fatalf("%s: summary=%+v title=%q want 1 + %q", name, got.Summary, got.Pages[0].Title, subdir)
			}
			var dbBody string
			if err := d.QueryRow(`SELECT body FROM pages WHERE id = ?`, got.Pages[0].ID).Scan(&dbBody); err != nil {
				t.Fatalf("body: %v", err)
			}
			if !strings.Contains(dbBody, "body for "+name) {
				t.Fatalf("%s: body=%q does not contain readme content", name, dbBody)
			}
		}
	})

	// 11. parent_id places imported tree under an existing page.
	t.Run("parent_id_target", func(t *testing.T) {
		// Seed an existing page in the space to serve as the import target.
		res, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
		                    VALUES (?, NULL, 'Mount', '', 50)`, space)
		if err != nil {
			t.Fatalf("seed mount: %v", err)
		}
		mountID, _ := res.LastInsertId()
		resp, body := postImport(t, adminC, ts.URL, space, &mountID, false, []importFilePart{
			{relPath: "child.md", body: "c"},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", resp.StatusCode, body)
		}
		got := decodeImportResp(t, body)
		if got.Pages[0].ParentID == nil || *got.Pages[0].ParentID != mountID {
			t.Fatalf("imported parent=%v want %d", got.Pages[0].ParentID, mountID)
		}
	})

	// 12. parent_id in a different space → 400 parent_space_mismatch.
	t.Run("parent_id_wrong_space", func(t *testing.T) {
		otherSpace := seedSpace(t, d, "Other", "other", admin)
		res, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
		                    VALUES (?, NULL, 'X', '', 0)`, otherSpace)
		if err != nil {
			t.Fatalf("seed cross-space: %v", err)
		}
		otherPageID, _ := res.LastInsertId()
		resp, body := postImport(t, adminC, ts.URL, space, &otherPageID, false, []importFilePart{
			{relPath: "x.md", body: "x"},
		})
		if resp.StatusCode != http.StatusBadRequest ||
			!strings.Contains(string(body), `"code":"parent_space_mismatch"`) {
			t.Fatalf("status=%d body=%s want 400 parent_space_mismatch", resp.StatusCode, body)
		}
	})

	// 13. Missing space → 404 space_not_found.
	t.Run("space_not_found", func(t *testing.T) {
		resp, body := postImport(t, adminC, ts.URL, 999999, nil, false, []importFilePart{
			{relPath: "x.md", body: "x"},
		})
		if resp.StatusCode != http.StatusNotFound ||
			!strings.Contains(string(body), `"code":"space_not_found"`) {
			t.Fatalf("status=%d body=%s want 404 space_not_found", resp.StatusCode, body)
		}
	})

	// 14. Empty file list → 400 bad_request.
	t.Run("no_files", func(t *testing.T) {
		resp, body := postImport(t, adminC, ts.URL, space, nil, false, nil)
		if resp.StatusCode != http.StatusBadRequest ||
			!strings.Contains(string(body), `"code":"bad_request"`) {
			t.Fatalf("status=%d body=%s want 400 bad_request", resp.StatusCode, body)
		}
	})

	// 15. Page-history seeded: every imported page has at least one revision
	//     with source='import'.
	t.Run("page_revisions_seeded", func(t *testing.T) {
		resp, body := postImport(t, adminC, ts.URL, space, nil, false, []importFilePart{
			{relPath: "Hist/README.md", body: "hist root"},
			{relPath: "Hist/child.md", body: "hist child"},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", resp.StatusCode, body)
		}
		got := decodeImportResp(t, body)
		for _, p := range got.Pages {
			var n int
			if err := d.QueryRow(
				`SELECT COUNT(*) FROM page_revisions WHERE page_id = ? AND source = 'import'`,
				p.ID).Scan(&n); err != nil {
				t.Fatalf("count revisions for page %d: %v", p.ID, err)
			}
			if n != 1 {
				t.Fatalf("page %d (%q) revisions=%d want 1 import-seeded", p.ID, p.Title, n)
			}
		}
	})
}
