package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestPageRevisions_FullFlow covers the M9.0 backend slice end-to-end through
// the wired stack: role gating on both endpoints, snapshot-on-save rules
// (body / title / both / no-op), cross-page leak protection, and pagination.
func TestPageRevisions_FullFlow(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	_ = seedUser(t, d, "carol", "carolpw12", false) // non-member
	space := seedSpace(t, d, "Test Space", "test-space", admin)
	otherSpace := seedSpace(t, d, "Other Space", "other-space", admin)
	seedMember(t, d, space, bob, roleViewer)

	res, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES (?, NULL, 'P', 'initial body', 0)`, space)
	if err != nil {
		t.Fatalf("seed page: %v", err)
	}
	pageID, _ := res.LastInsertId()

	// Seed a page in the OTHER space (admin is owner; bob/carol are not
	// members) to exercise the cross-space 403 path.
	res2, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                     VALUES (?, NULL, 'Other', 'other body', 0)`, otherSpace)
	if err != nil {
		t.Fatalf("seed other page: %v", err)
	}
	otherPageID, _ := res2.LastInsertId()

	adminC := loginClient(t, ts, "admin", "adminpw12")
	bobC := loginClient(t, ts, "bob", "bobpw1234")
	carolC := loginClient(t, ts, "carol", "carolpw12")

	listURL := fmt.Sprintf("%s/api/pages/%d/revisions", ts.URL, pageID)
	pageURL := fmt.Sprintf("%s/api/pages/%d", ts.URL, pageID)

	// 1. viewer bob GET /revisions → 403 viewer_no_write.
	resp, _ := bobC.Get(listURL)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"code":"viewer_no_write"`) {
		t.Fatalf("viewer GET status=%d body=%s want 403 viewer_no_write", resp.StatusCode, body)
	}

	// 2. non-member carol GET /revisions → 403 forbidden.
	resp, _ = carolC.Get(listURL)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"code":"forbidden"`) {
		t.Fatalf("non-member GET status=%d body=%s want 403 forbidden", resp.StatusCode, body)
	}

	// 3. baseline: empty revisions list for a page that hasn't been patched.
	if revs := getRevisions(t, adminC, listURL); len(revs) != 0 {
		t.Fatalf("baseline revisions count=%d, want 0", len(revs))
	}

	// 4. PATCH body change → exactly 1 revision row, source=manual,
	//    byte_size=len(new body), author_id=session user.
	resp, _ = patchJSON(adminC, pageURL, `{"body":"changed body one"}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("body PATCH status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()
	revs := getRevisions(t, adminC, listURL)
	if len(revs) != 1 {
		t.Fatalf("after body PATCH revisions=%d, want 1", len(revs))
	}
	if revs[0].Source != "manual" {
		t.Fatalf("revision source=%q want %q", revs[0].Source, "manual")
	}
	if revs[0].ByteSize != int64(len("changed body one")) {
		t.Fatalf("byte_size=%d want %d", revs[0].ByteSize, len("changed body one"))
	}
	if revs[0].AuthorID == nil || *revs[0].AuthorID != admin {
		t.Fatalf("author_id=%v want %d", revs[0].AuthorID, admin)
	}
	if revs[0].AuthorUsername == nil || *revs[0].AuthorUsername != "admin" {
		t.Fatalf("author_username=%v want admin", revs[0].AuthorUsername)
	}
	if revs[0].Title != "P" {
		t.Fatalf("revision title=%q want %q (PATCH only changed body)", revs[0].Title, "P")
	}

	// 5. PATCH title only → exactly 1 NEW revision row (total 2).
	resp, _ = patchJSON(adminC, pageURL, `{"title":"Renamed"}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("title PATCH status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()
	revs = getRevisions(t, adminC, listURL)
	if len(revs) != 2 {
		t.Fatalf("after title PATCH revisions=%d, want 2", len(revs))
	}
	if revs[0].Title != "Renamed" {
		t.Fatalf("newest revision title=%q want %q", revs[0].Title, "Renamed")
	}

	// 6. PATCH both body and title in one call → exactly 1 NEW row (total 3).
	resp, _ = patchJSON(adminC, pageURL, `{"title":"Renamed Again","body":"changed body two"}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("both PATCH status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()
	revs = getRevisions(t, adminC, listURL)
	if len(revs) != 3 {
		t.Fatalf("after both PATCH revisions=%d, want 3", len(revs))
	}

	// 7. No-op PATCH (body+title equal to existing) → 0 new rows.
	resp, _ = patchJSON(adminC, pageURL, `{"title":"Renamed Again","body":"changed body two"}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("no-op PATCH status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()
	revs = getRevisions(t, adminC, listURL)
	if len(revs) != 3 {
		t.Fatalf("after no-op PATCH revisions=%d, want still 3", len(revs))
	}

	// 8. GET /revisions/{rev_id} → 200 with full body+title+author_username.
	newestID := revs[0].ID
	resp, _ = adminC.Get(fmt.Sprintf("%s/%d", listURL, newestID))
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET single status=%d body=%s", resp.StatusCode, b)
	}
	var single struct {
		Revision struct {
			ID             int64   `json:"id"`
			Body           string  `json:"body"`
			Title          string  `json:"title"`
			AuthorUsername *string `json:"author_username"`
			Source         string  `json:"source"`
		} `json:"revision"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&single); err != nil {
		t.Fatalf("decode single: %v", err)
	}
	resp.Body.Close()
	if single.Revision.ID != newestID || single.Revision.Body != "changed body two" ||
		single.Revision.Title != "Renamed Again" || single.Revision.Source != "manual" {
		t.Fatalf("single revision body mismatch: %+v", single.Revision)
	}
	if single.Revision.AuthorUsername == nil || *single.Revision.AuthorUsername != "admin" {
		t.Fatalf("single revision author_username=%v want admin", single.Revision.AuthorUsername)
	}

	// 9. GET /revisions/{rev_id} with the wrong page in the URL → 404
	//    revision_not_found (don't leak revision existence).
	resp, _ = adminC.Get(fmt.Sprintf("%s/api/pages/%d/revisions/%d", ts.URL, otherPageID, newestID))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), `"code":"revision_not_found"`) {
		t.Fatalf("cross-page GET status=%d body=%s want 404 revision_not_found", resp.StatusCode, body)
	}

	// 10. GET /revisions on a page in a different space (admin is OWNER of
	//     both, so this verifies the per-page space gate works, not just
	//     "any membership"). Use bob who is only a viewer of `space` and a
	//     non-member of `otherSpace`. Both should 403, just different codes.
	otherListURL := fmt.Sprintf("%s/api/pages/%d/revisions", ts.URL, otherPageID)
	resp, _ = bobC.Get(otherListURL)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"code":"forbidden"`) {
		t.Fatalf("cross-space GET status=%d body=%s want 403 forbidden", resp.StatusCode, body)
	}

	// 11. Pagination: insert enough additional revisions so the page has 60+.
	//     We already have 3 from steps 4-6; PATCH 57 more times with unique
	//     bodies to push the total to 60.
	for i := 0; i < 57; i++ {
		body := fmt.Sprintf(`{"body":"pg %d"}`, i)
		resp, _ := patchJSON(adminC, pageURL, body)
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("pagination patch %d status=%d body=%s", i, resp.StatusCode, b)
		}
		resp.Body.Close()
	}

	first := getRevisions(t, adminC, listURL+"?limit=25")
	if len(first) != 25 {
		t.Fatalf("limit=25 returned %d, want 25", len(first))
	}
	// Confirm DESC ordering.
	for i := 1; i < len(first); i++ {
		if first[i].ID >= first[i-1].ID {
			t.Fatalf("revisions not DESC at index %d: %d >= %d", i, first[i].ID, first[i-1].ID)
		}
	}

	lastID := first[len(first)-1].ID
	second := getRevisions(t, adminC, fmt.Sprintf("%s?limit=25&cursor=%d", listURL, lastID))
	if len(second) != 25 {
		t.Fatalf("second page returned %d, want 25", len(second))
	}
	if second[0].ID >= lastID {
		t.Fatalf("second page first id=%d, want < cursor=%d", second[0].ID, lastID)
	}

	// Past the end: cursor=1 (every real revision id > 1) with no more rows
	// before id 1 → empty array, not error.
	tail := getRevisions(t, adminC, listURL+"?limit=25&cursor=1")
	if len(tail) != 0 {
		t.Fatalf("past-end returned %d, want 0", len(tail))
	}

	// 12. limit clamp: limit=500 should clamp to 200 max. We have 60 revs so
	//     this just verifies the cap doesn't reject. The 200-cap matters
	//     when callers eventually exceed it; here we just confirm no 400.
	resp, _ = adminC.Get(listURL + "?limit=500")
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("limit=500 clamp status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()
}

type revisionListItem struct {
	ID             int64   `json:"id"`
	PageID         int64   `json:"page_id"`
	Title          string  `json:"title"`
	AuthorID       *int64  `json:"author_id"`
	AuthorUsername *string `json:"author_username"`
	Source         string  `json:"source"`
	ByteSize       int64   `json:"byte_size"`
	CreatedAt      string  `json:"created_at"`
}

func getRevisions(t *testing.T, c *http.Client, url string) []revisionListItem {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("get revisions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("get revisions status=%d body=%s url=%s", resp.StatusCode, b, url)
	}
	var got struct {
		Revisions []revisionListItem `json:"revisions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode revisions: %v", err)
	}
	return got.Revisions
}
