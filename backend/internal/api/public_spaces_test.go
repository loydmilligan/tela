package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestPublicSpaces_FullFlow exercises space-level public visibility end-to-end:
// the owner-only flip, anonymous read of a public space (space/tree/page/.md),
// the 404 wall around private spaces, cross-space page isolation, and — the
// load-bearing guarantee — that making a space public never opens WRITE.
func TestPublicSpaces_FullFlow(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	pubSpace := seedSpace(t, d, "Blog", "blog", alice)
	seedMember(t, d, pubSpace, bob, roleEditor)

	// A page in the soon-to-be-public space + a page in a separate private space.
	var pubPage int64
	if err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position, props)
	                       VALUES ($1, NULL, 'Hello', 'public body here', 0, '{"tags":["x"]}') RETURNING id`,
		pubSpace).Scan(&pubPage); err != nil {
		t.Fatalf("seed public page: %v", err)
	}
	privSpace := seedSpace(t, d, "Private", "private", alice)
	var privPage int64
	if err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                       VALUES ($1, NULL, 'Secret', 'secret body', 0) RETURNING id`,
		privSpace).Scan(&privPage); err != nil {
		t.Fatalf("seed private page: %v", err)
	}

	anon := &http.Client{}
	pubSpaceURL := fmt.Sprintf("%s/api/public/spaces/%d", ts.URL, pubSpace)

	// --- Before publishing: the public API treats it as nonexistent (404). ---
	if r, _ := anon.Get(pubSpaceURL); r.StatusCode != http.StatusNotFound {
		t.Fatalf("anon GET private-space public API status=%d want 404", r.StatusCode)
	}

	// --- Owner-only flip. Editor bob is forbidden; owner alice succeeds. ---
	bobC := loginClient(t, ts, "bob", "bobpw1234")
	if r, _ := patchJSON(bobC, fmt.Sprintf("%s/api/spaces/%d", ts.URL, pubSpace),
		`{"visibility":"public"}`); r.StatusCode != http.StatusForbidden {
		t.Fatalf("editor flip visibility status=%d want 403", r.StatusCode)
	}
	aliceC := loginClient(t, ts, "alice", "alicepw12")
	resp, _ := patchJSON(aliceC, fmt.Sprintf("%s/api/spaces/%d", ts.URL, pubSpace),
		`{"visibility":"public"}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("owner flip visibility status=%d body=%s", resp.StatusCode, b)
	}
	var upd struct {
		Space struct {
			Visibility string `json:"visibility"`
		} `json:"space"`
	}
	json.NewDecoder(resp.Body).Decode(&upd)
	resp.Body.Close()
	if upd.Space.Visibility != "public" {
		t.Fatalf("after flip visibility=%q want public", upd.Space.Visibility)
	}

	// Bad visibility value → 400.
	if r, _ := patchJSON(aliceC, fmt.Sprintf("%s/api/spaces/%d", ts.URL, pubSpace),
		`{"visibility":"semi"}`); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid visibility status=%d want 400", r.StatusCode)
	}

	// --- Anonymous reads of the public space now succeed. ---
	resp, _ = anon.Get(pubSpaceURL)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anon GET public space status=%d want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Tree.
	resp, _ = anon.Get(pubSpaceURL + "/tree")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anon GET tree status=%d want 200", resp.StatusCode)
	}
	var tree struct {
		Pages []struct {
			ID int64 `json:"id"`
		} `json:"pages"`
	}
	json.NewDecoder(resp.Body).Decode(&tree)
	resp.Body.Close()
	if len(tree.Pages) != 1 || tree.Pages[0].ID != pubPage {
		t.Fatalf("tree=%+v want one page id=%d", tree.Pages, pubPage)
	}

	// Page JSON — body + public frontmatter present.
	resp, _ = anon.Get(fmt.Sprintf("%s/pages/%d", pubSpaceURL, pubPage))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anon GET public page status=%d want 200", resp.StatusCode)
	}
	var pg struct {
		Page struct {
			Body  string         `json:"body"`
			Props map[string]any `json:"props"`
		} `json:"page"`
	}
	json.NewDecoder(resp.Body).Decode(&pg)
	resp.Body.Close()
	if !strings.Contains(pg.Page.Body, "public body here") {
		t.Fatalf("public page body=%q missing content", pg.Page.Body)
	}
	if pg.Page.Props["tags"] == nil {
		t.Fatalf("public page props missing tags (frontmatter should be public)")
	}

	// Markdown — inline text/markdown carrying the canonical body.
	resp, _ = anon.Get(fmt.Sprintf("%s/pages/%d/md", pubSpaceURL, pubPage))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anon GET .md status=%d want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	mdBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf(".md content-type=%q want text/markdown", ct)
	}
	if !strings.Contains(string(mdBody), "public body here") {
		t.Fatalf(".md body missing content: %s", mdBody)
	}

	// --- Cross-space isolation: a private-space page id under the public space → 404. ---
	if r, _ := anon.Get(fmt.Sprintf("%s/pages/%d", pubSpaceURL, privPage)); r.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-space page leak status=%d want 404", r.StatusCode)
	}

	// --- The private space stays sealed to the public API. ---
	if r, _ := anon.Get(fmt.Sprintf("%s/api/public/spaces/%d/pages/%d", ts.URL, privSpace, privPage)); r.StatusCode != http.StatusNotFound {
		t.Fatalf("anon read private space page status=%d want 404", r.StatusCode)
	}

	// --- READ-ONLY GUARANTEE: a public space grants no write to anonymous callers. ---
	// PATCH the page body anonymously → blocked by the session middleware (401),
	// because /api/pages is not a public path and publicness adds no membership.
	req, _ := http.NewRequest(http.MethodPatch,
		fmt.Sprintf("%s/api/pages/%d", ts.URL, pubPage), strings.NewReader(`{"body":"hacked"}`))
	req.Header.Set("Content-Type", "application/json")
	if r, _ := anon.Do(req); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon PATCH page in public space status=%d want 401", r.StatusCode)
	}
	// And creating a page anonymously is likewise rejected.
	if r, _ := anon.Post(ts.URL+"/api/pages", "application/json",
		strings.NewReader(fmt.Sprintf(`{"space_id":%d,"title":"x","body":"y"}`, pubSpace))); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon POST page status=%d want 401", r.StatusCode)
	}
	// The body is unchanged after the write attempts.
	var body string
	if err := d.QueryRow(`SELECT body FROM pages WHERE id = $1`, pubPage).Scan(&body); err != nil {
		t.Fatalf("reload page: %v", err)
	}
	if body != "public body here" {
		t.Fatalf("page body mutated by anon write: %q", body)
	}
}
