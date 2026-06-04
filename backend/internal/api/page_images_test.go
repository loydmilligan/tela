package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
)

// postImage builds a multipart/form-data POST with a single "file" part.
func postImage(c *http.Client, u, filename string, data []byte) (*http.Response, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	mw.Close()
	req, err := http.NewRequest(http.MethodPost, u, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.Do(req)
}

type imageUploadResp struct {
	ID       int64  `json:"id"`
	PageID   int64  `json:"page_id"`
	Hash     string `json:"hash"`
	Mime     string `json:"mime"`
	ByteSize int64  `json:"byte_size"`
	URL      string `json:"url"`
}

// TestPageImages_FullFlow exercises the upload + public serve + auth gates of
// the image sidecar against the wired stack (routing, middleware, schema).
func TestPageImages_FullFlow(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	_ = seedUser(t, d, "carol", "carolpw12", false) // non-member
	space := seedSpace(t, d, "Test Space", "test-space", admin)
	seedMember(t, d, space, bob, roleViewer)

	res, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES (?, NULL, 'P', 'body', 0)`, space)
	if err != nil {
		t.Fatalf("seed page: %v", err)
	}
	pageID, _ := res.LastInsertId()

	adminC := loginClient(t, ts, "admin", "adminpw12")
	bobC := loginClient(t, ts, "bob", "bobpw1234")
	carolC := loginClient(t, ts, "carol", "carolpw12")

	postURL := fmt.Sprintf("%s/api/pages/%d/images", ts.URL, pageID)

	// 1. Editor happy path: 200, body shape, row exists.
	resp, err := postImage(adminC, postURL, "pixel.png", pngOnePixel)
	if err != nil {
		t.Fatalf("POST happy: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST happy status=%d body=%s", resp.StatusCode, b)
	}
	var first imageUploadResp
	if err := json.NewDecoder(resp.Body).Decode(&first); err != nil {
		t.Fatalf("decode happy: %v", err)
	}
	resp.Body.Close()
	if first.PageID != pageID || first.Mime != "image/png" ||
		first.ByteSize != int64(len(pngOnePixel)) ||
		first.URL != fmt.Sprintf("/api/images/%d/%s.png", pageID, first.Hash) {
		t.Fatalf("POST happy resp = %+v", first)
	}
	var dbCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM page_images WHERE page_id = ? AND content_hash = ?`,
		pageID, first.Hash).Scan(&dbCount); err != nil {
		t.Fatalf("count after POST: %v", err)
	}
	if dbCount != 1 {
		t.Fatalf("after POST page_images count=%d want 1", dbCount)
	}

	// 2. Idempotent re-upload: same bytes → same row, still count 1.
	resp2, err := postImage(adminC, postURL, "pixel.png", pngOnePixel)
	if err != nil {
		t.Fatalf("POST idempotent: %v", err)
	}
	var second imageUploadResp
	_ = json.NewDecoder(resp2.Body).Decode(&second)
	resp2.Body.Close()
	if second.ID != first.ID {
		t.Fatalf("idempotent id = %d want %d", second.ID, first.ID)
	}
	_ = d.QueryRow(`SELECT COUNT(*) FROM page_images WHERE page_id = ?`, pageID).Scan(&dbCount)
	if dbCount != 1 {
		t.Fatalf("after idempotent count=%d want 1", dbCount)
	}

	// 3. Public GET serves bytes with the stored mime, no session.
	getURL := fmt.Sprintf("%s/api/images/%d/%s.png", ts.URL, pageID, first.Hash)
	getResp, err := http.Get(getURL)
	if err != nil {
		t.Fatalf("public GET: %v", err)
	}
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("public GET status=%d", getResp.StatusCode)
	}
	if ct := getResp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("GET Content-Type=%q want image/png", ct)
	}
	gotBytes, _ := io.ReadAll(getResp.Body)
	getResp.Body.Close()
	if !bytes.Equal(gotBytes, pngOnePixel) {
		t.Fatalf("GET bytes mismatch (%d vs %d)", len(gotBytes), len(pngOnePixel))
	}

	// 4. GET unknown hash → 404 (and not a 400 enumeration oracle).
	missURL := fmt.Sprintf("%s/api/images/%d/%s.png", ts.URL, pageID,
		"0000000000000000000000000000000000000000000000000000000000000000")
	missResp, _ := http.Get(missURL)
	if missResp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET miss status=%d want 404", missResp.StatusCode)
	}
	missResp.Body.Close()

	// 5. Non-PNG/JPEG/GIF/WEBP bytes → 400 (server detects by magic bytes).
	badResp, err := postImage(adminC, postURL, "notes.txt", []byte("just some text, not an image"))
	if err != nil {
		t.Fatalf("POST bad type: %v", err)
	}
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST bad type status=%d want 400", badResp.StatusCode)
	}
	badResp.Body.Close()

	// 6. Viewer role → 403 viewer_no_write.
	vResp, err := postImage(bobC, postURL, "pixel.png", pngOnePixel)
	if err != nil {
		t.Fatalf("POST viewer: %v", err)
	}
	if vResp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST viewer status=%d want 403", vResp.StatusCode)
	}
	vResp.Body.Close()

	// 7. Non-member → 403.
	nmResp, err := postImage(carolC, postURL, "pixel.png", pngOnePixel)
	if err != nil {
		t.Fatalf("POST non-member: %v", err)
	}
	if nmResp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST non-member status=%d want 403", nmResp.StatusCode)
	}
	nmResp.Body.Close()
}
