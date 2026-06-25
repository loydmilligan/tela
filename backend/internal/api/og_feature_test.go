package api

import (
	"bytes"
	"image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestFeatureOG_Ask(t *testing.T) {
	ts, _ := newWiredServer(t)

	get := func(path string) (*http.Response, []byte) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		resp, err := http.DefaultClient.Do(req) // cookie-less: pins IsPublicPath bypass
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, b
	}

	t.Run("HTML_card", func(t *testing.T) {
		resp, body := get("/ask")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200 (must bypass auth)", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type=%q want text/html", ct)
		}
		s := string(body)
		for _, want := range []string{
			`og:title" content="Ask your docs`,
			`og:image" content=`,
			`/ask/og.png`,
			`og:type" content="website"`,
			`twitter:card" content="summary_large_image"`,
		} {
			if !strings.Contains(s, want) {
				t.Fatalf("OG HTML missing %q\n%s", want, s)
			}
		}
	})

	t.Run("og_png", func(t *testing.T) {
		resp, body := get("/ask/og.png")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
			t.Fatalf("Content-Type=%q want image/png", ct)
		}
		if img, err := png.Decode(bytes.NewReader(body)); err != nil {
			t.Fatalf("png decode: %v", err)
		} else if img.Bounds().Dx() != 1200 || img.Bounds().Dy() != 630 {
			t.Fatalf("bounds=%v want 1200x630", img.Bounds())
		}
	})

	t.Run("shared_question_card", func(t *testing.T) {
		// A shared ask link (?q=) features the question in the card, not the
		// generic "Ask your docs", and points og:image at the question render.
		_, body := get("/ask?q=How%20do%20I%20deploy%20tela%3F&demo=1")
		s := string(body)
		for _, want := range []string{
			`og:title" content="Ask: How do I deploy tela?`,
			`/ask/og.png?q=`,
		} {
			if !strings.Contains(s, want) {
				t.Fatalf("question OG HTML missing %q\n%s", want, s)
			}
		}
		resp, png := get("/ask/og.png?q=How%20do%20I%20deploy%20tela%3F")
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
			t.Fatalf("question og.png: status=%d ct=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		if len(png) < 5000 {
			t.Fatalf("question og.png too small (%d bytes)", len(png))
		}
	})

	// Optional visual dump for manual review.
	if dir := os.Getenv("OG_DUMP_DIR"); dir != "" {
		_, body := get("/ask/og.png")
		_ = os.WriteFile(dir+"/og-ask.png", body, 0o644)
	}
}
