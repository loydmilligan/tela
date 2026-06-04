package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// pageBodyItem mirrors pageBodyDTO for JSON decoding. Kept separate so the
// test file does not depend on internal struct visibility rules.
type pageBodyItem struct {
	ID        int64  `json:"id"`
	SpaceID   int64  `json:"space_id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	UpdatedAt string `json:"updated_at"`
}

type pageBodiesResp struct {
	Pages      []pageBodyItem `json:"pages"`
	NextCursor *int64         `json:"next_cursor"`
	HasMore    bool           `json:"has_more"`
}

type indexVersionResp struct {
	Version string `json:"version"`
}

// TestPageBodies_FullFlow exercises GET /api/pages/bodies end-to-end via the
// wired stack: membership gating (incl. viewer-OK), query validation, the
// `since` filter, cursor pagination, and the limit clamp.
func TestPageBodies_FullFlow(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	_ = seedUser(t, d, "eve", "evepw12345", false) // non-member
	space := seedSpace(t, d, "Test Space", "test-space", admin)
	seedMember(t, d, space, bob, roleViewer)

	adminC := loginClient(t, ts, "admin", "adminpw12")
	bobC := loginClient(t, ts, "bob", "bobpw1234")
	eveC := loginClient(t, ts, "eve", "evepw12345")

	bodiesURL := fmt.Sprintf("%s/api/pages/bodies", ts.URL)

	// 1. missing space_id → 400 invalid_query.
	resp, _ := adminC.Get(bodiesURL)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"invalid_query"`) {
		t.Fatalf("missing space_id status=%d body=%s want 400 invalid_query", resp.StatusCode, body)
	}

	// 2. bad space_id → 400 invalid_query.
	resp, _ = adminC.Get(bodiesURL + "?space_id=abc")
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"invalid_query"`) {
		t.Fatalf("bad space_id status=%d body=%s want 400 invalid_query", resp.StatusCode, body)
	}

	// 3. space_not_found.
	resp, _ = adminC.Get(bodiesURL + "?space_id=99999")
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), `"code":"space_not_found"`) {
		t.Fatalf("space_not_found status=%d body=%s want 404 space_not_found", resp.StatusCode, body)
	}

	// 4. non-member eve → 403 forbidden.
	resp, _ = eveC.Get(bodiesURL + fmt.Sprintf("?space_id=%d", space))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"code":"forbidden"`) {
		t.Fatalf("non-member status=%d body=%s want 403 forbidden", resp.StatusCode, body)
	}

	// 5. empty space → 200 with empty list.
	got := getBodies(t, adminC, bodiesURL+fmt.Sprintf("?space_id=%d", space))
	if len(got.Pages) != 0 || got.HasMore || got.NextCursor != nil {
		t.Fatalf("empty space got=%+v, want pages=[] has_more=false next_cursor=null", got)
	}

	// Seed 5 pages with distinct updated_at timestamps so the `since` filter
	// has bite. We pin updated_at via UPDATE with explicit datetime strings.
	pageIDs := make([]int64, 0, 5)
	for i := 0; i < 5; i++ {
		var id int64
		err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
		                    VALUES ($1, NULL, $2, $3, $4) RETURNING id`,
			space, fmt.Sprintf("Title %d", i), fmt.Sprintf("body content %d", i), int64(i)).Scan(&id)
		if err != nil {
			t.Fatalf("seed page %d: %v", i, err)
		}
		pageIDs = append(pageIDs, id)
	}
	// Pin updated_at: page i gets timestamp "2026-01-01 00:00:0i".
	timestamps := []string{
		"2026-01-01 00:00:00",
		"2026-01-01 00:00:01",
		"2026-01-01 00:00:02",
		"2026-01-01 00:00:03",
		"2026-01-01 00:00:04",
	}
	for i, id := range pageIDs {
		if _, err := d.Exec(`UPDATE pages SET updated_at = $1 WHERE id = $2`, timestamps[i], id); err != nil {
			t.Fatalf("pin updated_at for page %d: %v", id, err)
		}
	}

	// 6. happy path: admin lists all five pages with slim DTO fields.
	got = getBodies(t, adminC, bodiesURL+fmt.Sprintf("?space_id=%d", space))
	if len(got.Pages) != 5 || got.HasMore || got.NextCursor != nil {
		t.Fatalf("admin list got len=%d has_more=%t next_cursor=%v, want 5/false/null",
			len(got.Pages), got.HasMore, got.NextCursor)
	}
	for i, p := range got.Pages {
		if p.SpaceID != space {
			t.Fatalf("row %d space_id=%d, want %d", i, p.SpaceID, space)
		}
		if p.Title != fmt.Sprintf("Title %d", i) {
			t.Fatalf("row %d title=%q", i, p.Title)
		}
		if p.Body != fmt.Sprintf("body content %d", i) {
			t.Fatalf("row %d body=%q", i, p.Body)
		}
		if p.UpdatedAt != timestamps[i] {
			t.Fatalf("row %d updated_at=%q want %q", i, p.UpdatedAt, timestamps[i])
		}
	}

	// 6b. Verify the response JSON does NOT include parent_id, position, or
	//     created_at — the slim DTO contract.
	resp, _ = adminC.Get(bodiesURL + fmt.Sprintf("?space_id=%d", space))
	rawBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, banned := range []string{`"parent_id"`, `"position"`, `"created_at"`} {
		if strings.Contains(string(rawBody), banned) {
			t.Fatalf("slim DTO leaked %s: %s", banned, rawBody)
		}
	}

	// 7. viewer bob CAN read the bodies endpoint (member+, NOT editor+).
	got = getBodies(t, bobC, bodiesURL+fmt.Sprintf("?space_id=%d", space))
	if len(got.Pages) != 5 {
		t.Fatalf("viewer bob got len=%d, want 5 (viewer must be allowed to read bodies)", len(got.Pages))
	}

	// 8. since filters out older pages.
	// since = first row's updated_at → returns rows 1..4 (id 2..5 from this fixture).
	got = getBodies(t, adminC, bodiesURL+fmt.Sprintf("?space_id=%d&since=%s", space, urlEscape(timestamps[0])))
	if len(got.Pages) != 4 {
		t.Fatalf("since=t0 got len=%d, want 4", len(got.Pages))
	}
	// since = last row's updated_at → returns 0 rows (strict > comparison).
	got = getBodies(t, adminC, bodiesURL+fmt.Sprintf("?space_id=%d&since=%s", space, urlEscape(timestamps[4])))
	if len(got.Pages) != 0 {
		t.Fatalf("since=t4 got len=%d, want 0", len(got.Pages))
	}

	// 9. bad since → 400 bad_request.
	resp, _ = adminC.Get(bodiesURL + fmt.Sprintf("?space_id=%d&since=garbage", space))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"bad_request"`) {
		t.Fatalf("bad since status=%d body=%s want 400 bad_request", resp.StatusCode, body)
	}

	// 10. bad cursor.
	for _, bad := range []string{"abc", "-1"} {
		resp, _ = adminC.Get(bodiesURL + fmt.Sprintf("?space_id=%d&cursor=%s", space, bad))
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"bad_request"`) {
			t.Fatalf("bad cursor=%q status=%d body=%s want 400 bad_request", bad, resp.StatusCode, body)
		}
	}

	// 11. bad limit.
	for _, bad := range []string{"0", "abc", "-3"} {
		resp, _ = adminC.Get(bodiesURL + fmt.Sprintf("?space_id=%d&limit=%s", space, bad))
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"bad_request"`) {
			t.Fatalf("bad limit=%q status=%d body=%s want 400 bad_request", bad, resp.StatusCode, body)
		}
	}

	// 12. limit clamp: limit=9999 should not error and should return at most
	//     pageBodiesMaxLimit rows (we only have 5, so it returns 5).
	got = getBodies(t, adminC, bodiesURL+fmt.Sprintf("?space_id=%d&limit=9999", space))
	if len(got.Pages) != 5 {
		t.Fatalf("limit=9999 clamp got len=%d, want 5", len(got.Pages))
	}

	// 13. pagination: limit=2 → 2 rows + has_more=true + next_cursor set;
	//                 cursor advances to next 2 rows; final → 1 row +
	//                 has_more=false + next_cursor=null.
	first := getBodies(t, adminC, bodiesURL+fmt.Sprintf("?space_id=%d&limit=2", space))
	if len(first.Pages) != 2 || !first.HasMore || first.NextCursor == nil {
		t.Fatalf("page 1 got len=%d has_more=%t next_cursor=%v, want 2/true/set",
			len(first.Pages), first.HasMore, first.NextCursor)
	}
	if *first.NextCursor != first.Pages[1].ID {
		t.Fatalf("next_cursor=%d, want last page id=%d", *first.NextCursor, first.Pages[1].ID)
	}
	if first.Pages[0].ID != pageIDs[0] || first.Pages[1].ID != pageIDs[1] {
		t.Fatalf("page 1 ids=[%d,%d], want [%d,%d]",
			first.Pages[0].ID, first.Pages[1].ID, pageIDs[0], pageIDs[1])
	}

	second := getBodies(t, adminC, bodiesURL+fmt.Sprintf("?space_id=%d&limit=2&cursor=%d", space, *first.NextCursor))
	if len(second.Pages) != 2 || !second.HasMore || second.NextCursor == nil {
		t.Fatalf("page 2 got len=%d has_more=%t next_cursor=%v, want 2/true/set",
			len(second.Pages), second.HasMore, second.NextCursor)
	}
	if second.Pages[0].ID != pageIDs[2] || second.Pages[1].ID != pageIDs[3] {
		t.Fatalf("page 2 ids=[%d,%d], want [%d,%d]",
			second.Pages[0].ID, second.Pages[1].ID, pageIDs[2], pageIDs[3])
	}

	third := getBodies(t, adminC, bodiesURL+fmt.Sprintf("?space_id=%d&limit=2&cursor=%d", space, *second.NextCursor))
	if len(third.Pages) != 1 || third.HasMore || third.NextCursor != nil {
		t.Fatalf("page 3 got len=%d has_more=%t next_cursor=%v, want 1/false/null",
			len(third.Pages), third.HasMore, third.NextCursor)
	}
	if third.Pages[0].ID != pageIDs[4] {
		t.Fatalf("page 3 id=%d, want %d", third.Pages[0].ID, pageIDs[4])
	}

	// 14. past-end cursor → 200, empty, no next, has_more=false.
	past := getBodies(t, adminC, bodiesURL+fmt.Sprintf("?space_id=%d&cursor=%d", space, pageIDs[4]+9999))
	if len(past.Pages) != 0 || past.HasMore || past.NextCursor != nil {
		t.Fatalf("past-end got=%+v, want empty/false/null", past)
	}
}

// TestSpaceIndexVersion_FullFlow exercises GET /api/spaces/{id}/index-version
// end-to-end: role gates (viewer-OK), empty-space sentinel, PATCH bump, and
// DELETE bump.
func TestSpaceIndexVersion_FullFlow(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	_ = seedUser(t, d, "eve", "evepw12345", false)
	space := seedSpace(t, d, "Test Space", "test-space", admin)
	seedMember(t, d, space, bob, roleViewer)

	adminC := loginClient(t, ts, "admin", "adminpw12")
	bobC := loginClient(t, ts, "bob", "bobpw1234")
	eveC := loginClient(t, ts, "eve", "evepw12345")

	versionURL := fmt.Sprintf("%s/api/spaces/%d/index-version", ts.URL, space)

	// 1. empty space → "empty".
	v := getVersion(t, adminC, versionURL)
	if v != "empty" {
		t.Fatalf("empty space version=%q, want 'empty'", v)
	}

	// 2. non-member eve → 403 forbidden.
	resp, _ := eveC.Get(versionURL)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"code":"forbidden"`) {
		t.Fatalf("non-member status=%d body=%s want 403 forbidden", resp.StatusCode, body)
	}

	// 3. space_not_found.
	resp, _ = adminC.Get(fmt.Sprintf("%s/api/spaces/%d/index-version", ts.URL, 99999))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), `"code":"space_not_found"`) {
		t.Fatalf("space_not_found status=%d body=%s want 404 space_not_found", resp.StatusCode, body)
	}

	// Seed two pages with known updated_at; max is "2026-02-01 00:00:01".
	var pageA int64
	err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES ($1, NULL, 'A', 'a', 0) RETURNING id`, space).Scan(&pageA)
	if err != nil {
		t.Fatalf("seed page A: %v", err)
	}
	var pageB int64
	err = d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES ($1, NULL, 'B', 'b', 1) RETURNING id`, space).Scan(&pageB)
	if err != nil {
		t.Fatalf("seed page B: %v", err)
	}
	if _, err := d.Exec(`UPDATE pages SET updated_at = $1 WHERE id = $2`, "2026-02-01 00:00:00", pageA); err != nil {
		t.Fatalf("pin A: %v", err)
	}
	if _, err := d.Exec(`UPDATE pages SET updated_at = $1 WHERE id = $2`, "2026-02-01 00:00:01", pageB); err != nil {
		t.Fatalf("pin B: %v", err)
	}

	// 4. happy path: admin sees MAX(updated_at).
	v = getVersion(t, adminC, versionURL)
	if v != "2026-02-01 00:00:01" {
		t.Fatalf("admin version=%q, want '2026-02-01 00:00:01'", v)
	}

	// Cross-check that the value matches a raw query on the DB so the
	// computation is honest, not a coincidence.
	var dbMax string
	if err := d.QueryRow(`SELECT MAX(updated_at) FROM pages WHERE space_id = $1`, space).Scan(&dbMax); err != nil {
		t.Fatalf("raw max query: %v", err)
	}
	if v != dbMax {
		t.Fatalf("version=%q, dbMax=%q, want equal", v, dbMax)
	}

	// 5. viewer bob CAN read index-version.
	v = getVersion(t, bobC, versionURL)
	if v != "2026-02-01 00:00:01" {
		t.Fatalf("viewer bob version=%q, want '2026-02-01 00:00:01' (viewer must be allowed)", v)
	}

	// 6. PATCH bumps the version. We mutate via the live endpoint so the
	//    test actually exercises the PATCH→updated_at→index-version chain
	//    (not a backdoor UPDATE).
	v1 := getVersion(t, adminC, versionURL)
	resp, _ = patchJSON(adminC, fmt.Sprintf("%s/api/pages/%d", ts.URL, pageA), `{"body":"a updated"}`)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("patch page A status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()
	v2 := getVersion(t, adminC, versionURL)
	if v1 == v2 {
		t.Fatalf("version did not change after PATCH: v1=%q v2=%q", v1, v2)
	}

	// 7. DELETE bumps the version. Delete page A (whose updated_at was
	//    bumped to "now" by step 6 above and is therefore the MAX); the
	//    space MAX(updated_at) drops to page B's pinned older value.
	resp, _ = deleteReq(adminC, fmt.Sprintf("%s/api/pages/%d", ts.URL, pageA))
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete page A status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()
	v3 := getVersion(t, adminC, versionURL)
	if v3 == v2 {
		t.Fatalf("version did not change after DELETE: v2=%q v3=%q", v2, v3)
	}
	if v3 != "2026-02-01 00:00:01" {
		t.Fatalf("after deleting page A version=%q, want page B's pinned mtime '2026-02-01 00:00:01'", v3)
	}

	// 8. After deleting both pages the sentinel returns to "empty".
	resp, _ = deleteReq(adminC, fmt.Sprintf("%s/api/pages/%d", ts.URL, pageB))
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete page B status=%d body=%s", resp.StatusCode, b)
	}
	resp.Body.Close()
	v4 := getVersion(t, adminC, versionURL)
	if v4 != "empty" {
		t.Fatalf("after deleting all pages version=%q, want 'empty'", v4)
	}
}

func getBodies(t *testing.T, c *http.Client, url string) pageBodiesResp {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("get bodies %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("get bodies %s status=%d body=%s", url, resp.StatusCode, b)
	}
	var got pageBodiesResp
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode bodies: %v", err)
	}
	return got
}

func getVersion(t *testing.T, c *http.Client, url string) string {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("get version %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("get version %s status=%d body=%s", url, resp.StatusCode, b)
	}
	var got indexVersionResp
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode version: %v", err)
	}
	return got.Version
}

// urlEscape replaces the single space character with '+' so the test can
// pass datetime strings through query params without dragging net/url in.
// Sufficient for the fixed-format `YYYY-MM-DD HH:MM:SS` fixtures used here.
func urlEscape(s string) string {
	return strings.ReplaceAll(s, " ", "+")
}
