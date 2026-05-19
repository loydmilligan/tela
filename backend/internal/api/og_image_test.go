package api

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestOGImage_FullFlow exercises GET /p/{id}/og.png across the eight scenarios
// documented in the M11.1 brief: real-browser fetch, bot fetch, missing page,
// non-numeric id, conditional-get hit and stale, long-title rendering, and
// the auth-middleware bypass for cookie-less clients.
func TestOGImage_FullFlow(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "Engineering", "engineering", admin)

	res, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
	                    VALUES (?, NULL, 'Welcome', 'hello body', 0)`, space)
	if err != nil {
		t.Fatalf("seed page: %v", err)
	}
	pageID, _ := res.LastInsertId()

	pngMagic := []byte("\x89PNG\r\n\x1a\n")

	get := func(t *testing.T, c *http.Client, path string, headers map[string]string) (*http.Response, []byte) {
		t.Helper()
		if c == nil {
			c = http.DefaultClient
		}
		req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		if err != nil {
			t.Fatalf("build req: %v", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("do req: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp, body
	}

	t.Run("OK_RealBrowser", func(t *testing.T) {
		resp, body := get(t, nil, fmt.Sprintf("/p/%d/og.png", pageID), nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
			t.Fatalf("Content-Type=%q want image/png", ct)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=3600" {
			t.Fatalf("Cache-Control=%q want 'public, max-age=3600'", cc)
		}
		if et := resp.Header.Get("ETag"); et == "" {
			t.Fatalf("ETag missing")
		}
		if !bytes.HasPrefix(body, pngMagic) {
			t.Fatalf("body does not start with PNG magic; first16=%x", body[:min(16, len(body))])
		}
		if len(body) < 5000 {
			t.Fatalf("body length=%d, expected > 5000 for a 1200x630 rendered PNG", len(body))
		}
		img, err := png.Decode(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("png decode: %v", err)
		}
		want := image.Rect(0, 0, 1200, 630)
		if img.Bounds() != want {
			t.Fatalf("bounds=%v want %v", img.Bounds(), want)
		}
	})

	t.Run("OK_BotUA", func(t *testing.T) {
		resp, body := get(t, nil, fmt.Sprintf("/p/%d/og.png", pageID), map[string]string{
			"User-Agent": "Slackbot-LinkExpanding 1.0",
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200 (handler must not UA-branch)", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
			t.Fatalf("Content-Type=%q want image/png", ct)
		}
		if !bytes.HasPrefix(body, pngMagic) {
			t.Fatalf("body does not start with PNG magic")
		}
	})

	t.Run("NotFound_MissingPage", func(t *testing.T) {
		resp, body := get(t, nil, "/p/99999/og.png", nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status=%d want 404", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type=%q want text/html prefix", ct)
		}
		if !strings.Contains(string(body), "Not found") {
			t.Fatalf("body=%s missing 'Not found'", body)
		}
	})

	t.Run("NotFound_NonNumericID", func(t *testing.T) {
		resp, _ := get(t, nil, "/p/abc/og.png", nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status=%d want 404", resp.StatusCode)
		}
	})

	t.Run("ConditionalGet_IfNoneMatch_Hit", func(t *testing.T) {
		resp, _ := get(t, nil, fmt.Sprintf("/p/%d/og.png", pageID), nil)
		etag := resp.Header.Get("ETag")
		if etag == "" {
			t.Fatalf("first request: ETag missing")
		}
		resp2, body2 := get(t, nil, fmt.Sprintf("/p/%d/og.png", pageID), map[string]string{
			"If-None-Match": etag,
		})
		if resp2.StatusCode != http.StatusNotModified {
			t.Fatalf("status=%d want 304", resp2.StatusCode)
		}
		if len(body2) != 0 {
			t.Fatalf("body len=%d want 0 on 304", len(body2))
		}
		if got := resp2.Header.Get("ETag"); got != etag {
			t.Fatalf("304 ETag=%q want %q", got, etag)
		}
		if cc := resp2.Header.Get("Cache-Control"); cc != "public, max-age=3600" {
			t.Fatalf("304 Cache-Control=%q want 'public, max-age=3600'", cc)
		}
	})

	t.Run("ConditionalGet_IfNoneMatch_Stale", func(t *testing.T) {
		resp, _ := get(t, nil, fmt.Sprintf("/p/%d/og.png", pageID), nil)
		oldEtag := resp.Header.Get("ETag")
		if oldEtag == "" {
			t.Fatalf("first request: ETag missing")
		}

		// Bump updated_at directly. Editing via PATCH would also work, but a
		// direct UPDATE keeps the test independent of the pages handler.
		if _, err := d.Exec(
			`UPDATE pages SET updated_at = datetime('now','+1 hour') WHERE id = ?`, pageID,
		); err != nil {
			t.Fatalf("bump updated_at: %v", err)
		}

		resp2, body2 := get(t, nil, fmt.Sprintf("/p/%d/og.png", pageID), map[string]string{
			"If-None-Match": oldEtag,
		})
		if resp2.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200 (stale ETag should not 304)", resp2.StatusCode)
		}
		if newEtag := resp2.Header.Get("ETag"); newEtag == "" || newEtag == oldEtag {
			t.Fatalf("new ETag=%q, old=%q — must differ after updated_at bump", newEtag, oldEtag)
		}
		if !bytes.HasPrefix(body2, pngMagic) {
			t.Fatalf("body does not start with PNG magic")
		}
	})

	t.Run("LongTitle_Truncated", func(t *testing.T) {
		longTitle := strings.Repeat("Lorem ipsum dolor sit amet ", 50)
		res2, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
		                     VALUES (?, NULL, ?, '', 1)`, space, longTitle)
		if err != nil {
			t.Fatalf("seed long page: %v", err)
		}
		longID, _ := res2.LastInsertId()

		resp, body := get(t, nil, fmt.Sprintf("/p/%d/og.png", longID), nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
			t.Fatalf("Content-Type=%q want image/png", ct)
		}
		img, err := png.Decode(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("png decode of long-title render: %v", err)
		}
		if img.Bounds() != image.Rect(0, 0, 1200, 630) {
			t.Fatalf("bounds=%v want 1200x630", img.Bounds())
		}
	})

	t.Run("BypassesAuth", func(t *testing.T) {
		// http.DefaultClient has no cookie jar — if the middleware bypass for
		// /p/* were missing, the request would 401 here. The other subtests
		// already use cookie-less clients; this one pins the assertion
		// explicitly so a future middleware change is loud.
		resp, _ := get(t, nil, fmt.Sprintf("/p/%d/og.png", pageID), nil)
		if resp.StatusCode == http.StatusUnauthorized {
			t.Fatalf("middleware bypass for /p/* missing — cookie-less request returned 401")
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200", resp.StatusCode)
		}
	})
}

