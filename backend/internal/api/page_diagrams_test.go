package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// pngOnePixel is the smallest valid PNG (a transparent 1x1) — enough to pass
// the magic-byte check without bloating the fixture.
var pngOnePixel = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x62, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

func putJSON(c *http.Client, u, body string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPut, u, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.Do(req)
}

type diagramUploadResp struct {
	ID        int64  `json:"id"`
	PageID    int64  `json:"page_id"`
	SceneHash string `json:"scene_hash"`
	ByteSize  int64  `json:"byte_size"`
	URL       string `json:"url"`
}

// TestPageDiagrams_FullFlow exercises every load-bearing assertion of M13.2
// against the wired stack so the routing, middleware, and DB schema all
// participate. Each numbered block maps to a test case enumerated in the
// task brief (1..13).
func TestPageDiagrams_FullFlow(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	_ = seedUser(t, d, "carol", "carolpw12", false) // non-member
	space := seedSpace(t, d, "Test Space", "test-space", admin)
	seedMember(t, d, space, bob, roleViewer)

	var pageID int64
	err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES ($1, NULL, 'P', 'body', 0) RETURNING id`, space).Scan(&pageID)
	if err != nil {
		t.Fatalf("seed page: %v", err)
	}

	adminC := loginClient(t, ts, "admin", "adminpw12")
	bobC := loginClient(t, ts, "bob", "bobpw1234")
	carolC := loginClient(t, ts, "carol", "carolpw12")

	putURL := fmt.Sprintf("%s/api/pages/%d/diagrams", ts.URL, pageID)
	pngB64 := base64.StdEncoding.EncodeToString(pngOnePixel)
	const hash = "abcdef0123456789"
	body := fmt.Sprintf(`{"scene_hash":%q,"png_base64":%q}`, hash, pngB64)

	// 1. PUT happy path: editor uploads, get 200 + body shape, row exists.
	resp, err := putJSON(adminC, putURL, body)
	if err != nil {
		t.Fatalf("PUT happy: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT happy status=%d body=%s", resp.StatusCode, b)
	}
	var firstResp diagramUploadResp
	if err := json.NewDecoder(resp.Body).Decode(&firstResp); err != nil {
		t.Fatalf("decode happy: %v", err)
	}
	resp.Body.Close()
	if firstResp.PageID != pageID || firstResp.SceneHash != hash ||
		firstResp.ByteSize != int64(len(pngOnePixel)) ||
		firstResp.URL != fmt.Sprintf("/api/diagrams/%d/%s.png", pageID, hash) {
		t.Fatalf("PUT happy resp = %+v", firstResp)
	}
	var dbCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM page_diagrams WHERE page_id = $1 AND scene_hash = $2`,
		pageID, hash).Scan(&dbCount); err != nil {
		t.Fatalf("count after PUT: %v", err)
	}
	if dbCount != 1 {
		t.Fatalf("after PUT page_diagrams count=%d want 1", dbCount)
	}

	// 2. PUT idempotent upsert: second PUT with same hash returns the same id.
	resp, err = putJSON(adminC, putURL, body)
	if err != nil {
		t.Fatalf("PUT idempotent: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT idempotent status=%d body=%s", resp.StatusCode, b)
	}
	var secondResp diagramUploadResp
	if err := json.NewDecoder(resp.Body).Decode(&secondResp); err != nil {
		t.Fatalf("decode idempotent: %v", err)
	}
	resp.Body.Close()
	if secondResp.ID != firstResp.ID {
		t.Fatalf("idempotent PUT id=%d, first id=%d — must be equal", secondResp.ID, firstResp.ID)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM page_diagrams WHERE page_id = $1 AND scene_hash = $2`,
		pageID, hash).Scan(&dbCount); err != nil {
		t.Fatalf("count after idempotent: %v", err)
	}
	if dbCount != 1 {
		t.Fatalf("after idempotent PUT count=%d want still 1", dbCount)
	}

	// 3. PUT viewer 403 viewer_no_write.
	resp, _ = putJSON(bobC, putURL, body)
	gotBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(gotBody), `"code":"viewer_no_write"`) {
		t.Fatalf("viewer PUT status=%d body=%s want 403 viewer_no_write", resp.StatusCode, gotBody)
	}

	// 4. PUT non-member 403 forbidden.
	resp, _ = putJSON(carolC, putURL, body)
	gotBody, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(gotBody), `"code":"forbidden"`) {
		t.Fatalf("non-member PUT status=%d body=%s want 403 forbidden", resp.StatusCode, gotBody)
	}

	// 5. PUT anon 401 — fresh client with no cookie jar.
	resp, _ = putJSON(&http.Client{}, putURL, body)
	gotBody, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(gotBody), `"code":"unauthorized"`) {
		t.Fatalf("anon PUT status=%d body=%s want 401 unauthorized", resp.StatusCode, gotBody)
	}

	// 6. PUT rejects non-PNG bytes ("hello" → 400 bad_request).
	helloB64 := base64.StdEncoding.EncodeToString([]byte("hello"))
	resp, _ = putJSON(adminC, putURL,
		fmt.Sprintf(`{"scene_hash":"deadbeef","png_base64":%q}`, helloB64))
	gotBody, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(gotBody), `"code":"bad_request"`) {
		t.Fatalf("non-PNG PUT status=%d body=%s want 400 bad_request", resp.StatusCode, gotBody)
	}

	// 7. PUT rejects bad hash format.
	resp, _ = putJSON(adminC, putURL,
		fmt.Sprintf(`{"scene_hash":"xyz!","png_base64":%q}`, pngB64))
	gotBody, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(gotBody), `"code":"bad_request"`) {
		t.Fatalf("bad-hash PUT status=%d body=%s want 400 bad_request", resp.StatusCode, gotBody)
	}

	// 8. GET happy path: served with correct headers + body matches.
	getURL := fmt.Sprintf("%s/api/diagrams/%d/%s.png", ts.URL, pageID, hash)
	resp, err = adminC.Get(getURL)
	if err != nil {
		t.Fatalf("GET happy: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET happy status=%d body=%s", resp.StatusCode, b)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("GET happy Content-Type=%q want image/png", ct)
	}
	if et := resp.Header.Get("ETag"); et != `"`+hash+`"` {
		t.Fatalf("GET happy ETag=%q want %q", et, `"`+hash+`"`)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("GET happy Cache-Control=%q", cc)
	}
	if x := resp.Header.Get("X-Content-Type-Options"); x != "nosniff" {
		t.Fatalf("GET happy X-Content-Type-Options=%q want nosniff", x)
	}
	gotPNG, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(gotPNG, pngOnePixel) {
		t.Fatalf("GET happy body len=%d want %d (bytes mismatch)", len(gotPNG), len(pngOnePixel))
	}

	// 9. GET 304 on If-None-Match.
	req, _ := http.NewRequest(http.MethodGet, getURL, nil)
	req.Header.Set("If-None-Match", `"`+hash+`"`)
	resp, err = adminC.Do(req)
	if err != nil {
		t.Fatalf("GET 304: %v", err)
	}
	if resp.StatusCode != http.StatusNotModified {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("If-None-Match status=%d body=%s want 304", resp.StatusCode, b)
	}
	bodyOn304, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(bodyOn304) != 0 {
		t.Fatalf("304 body len=%d want 0", len(bodyOn304))
	}

	// 10. GET 404 on unknown hash.
	unknownURL := fmt.Sprintf("%s/api/diagrams/%d/00000000.png", ts.URL, pageID)
	resp, _ = adminC.Get(unknownURL)
	gotBody, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(gotBody), `"code":"not_found"`) {
		t.Fatalf("unknown-hash GET status=%d body=%s want 404 not_found", resp.StatusCode, gotBody)
	}

	// 11. GET 404 on non-hex hash (path-traversal defense).
	badHashURL := fmt.Sprintf("%s/api/diagrams/%d/notacceptable.png", ts.URL, pageID)
	resp, _ = adminC.Get(badHashURL)
	gotBody, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(gotBody), `"code":"not_found"`) {
		t.Fatalf("bad-hex GET status=%d body=%s want 404 not_found", resp.StatusCode, gotBody)
	}

	// 12. GET requires NO session — anon http.Client must still 200. This is
	//     the load-bearing public-no-auth assertion.
	resp, err = http.Get(getURL)
	if err != nil {
		t.Fatalf("anon GET: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("anon GET status=%d body=%s want 200 (public path)", resp.StatusCode, b)
	}
	anonBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(anonBytes, pngOnePixel) {
		t.Fatalf("anon GET body mismatch")
	}

	// 13. CASCADE on page delete: row gone after DELETE /api/pages/{id}.
	resp, err = deleteReq(adminC, fmt.Sprintf("%s/api/pages/%d", ts.URL, pageID))
	if err != nil {
		t.Fatalf("delete page: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete page status=%d want 204", resp.StatusCode)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM page_diagrams WHERE page_id = $1`, pageID).Scan(&dbCount); err != nil {
		t.Fatalf("count after page delete: %v", err)
	}
	if dbCount != 0 {
		t.Fatalf("after page DELETE page_diagrams count=%d want 0 (CASCADE failed)", dbCount)
	}
}
