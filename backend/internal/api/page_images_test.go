package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// TestPageImages_Serve covers the public serve path of the legacy page_images
// store. The upload route was retired in favor of the unified space_files
// attachments store; this route stays so images already embedded in historical
// page bodies (/api/images/{page}/{hash}) keep resolving. The row is seeded
// directly — there is no upload endpoint to create it anymore.
func TestPageImages_Serve(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "Test Space", "test-space", admin)

	var pageID int64
	if err := d.QueryRow(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                       VALUES ($1, NULL, 'P', 'body', 0) RETURNING id`, space).Scan(&pageID); err != nil {
		t.Fatalf("seed page: %v", err)
	}
	sum := sha256.Sum256(pngOnePixel)
	hash := hex.EncodeToString(sum[:])
	if _, err := d.Exec(`INSERT INTO page_images (page_id, content_hash, mime, data, byte_size)
	                     VALUES ($1, $2, 'image/png', $3, $4)`,
		pageID, hash, pngOnePixel, len(pngOnePixel)); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	// Public GET serves the bytes with the stored mime, no session.
	getURL := fmt.Sprintf("%s/api/images/%d/%s.png", ts.URL, pageID, hash)
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

	// Unknown hash → 404 (and not a 400 enumeration oracle).
	missURL := fmt.Sprintf("%s/api/images/%d/%s.png", ts.URL, pageID,
		"0000000000000000000000000000000000000000000000000000000000000000")
	missResp, err := http.Get(missURL)
	if err != nil {
		t.Fatalf("GET miss: %v", err)
	}
	if missResp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET miss status=%d want 404", missResp.StatusCode)
	}
	missResp.Body.Close()
}
