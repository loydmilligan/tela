package api

import (
	"bytes"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
)

func ogGet(t *testing.T, base, path string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+path, nil)
	resp, err := http.DefaultClient.Do(req) // cookieless: also pins the IsPublicPath bypass
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func assertPNG(t *testing.T, base, path string) {
	t.Helper()
	resp, body := ogGet(t, base, path)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("%s: status=%d ct=%q", path, resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if img, err := png.Decode(bytes.NewReader(body)); err != nil {
		t.Fatalf("%s: png decode: %v", path, err)
	} else if img.Bounds().Dx() != 1200 || img.Bounds().Dy() != 630 {
		t.Fatalf("%s: bounds=%v want 1200x630", path, img.Bounds())
	}
}

func TestFeatureOG_GraphDiscover(t *testing.T) {
	ts, _ := newWiredServer(t)
	for _, c := range []struct{ path, want string }{
		{"/graph", "Knowledge graph"},
		{"/discover", "Discover"},
	} {
		resp, body := ogGet(t, ts.URL, c.path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status=%d want 200", c.path, resp.StatusCode)
		}
		if !strings.Contains(string(body), `og:title" content="`+c.want) {
			t.Fatalf("%s: og:title missing %q\n%s", c.path, c.want, body)
		}
		assertPNG(t, ts.URL, c.path+"/og.png")
	}
}

func TestSpaceOG(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	sid := seedSpace(t, d, "Engineering Wiki", "engineering-wiki", admin)

	resp, body := ogGet(t, ts.URL, fmt.Sprintf("/spaces/%d", sid))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("space OG status=%d want 200", resp.StatusCode)
	}
	s := string(body)
	if !strings.Contains(s, `og:title" content="Engineering Wiki`) {
		t.Fatalf("space OG missing the space name in og:title\n%s", s)
	}
	if !strings.Contains(s, fmt.Sprintf("/spaces/%d/og.png", sid)) {
		t.Fatalf("space OG missing og:image pointing at the space card\n%s", s)
	}
	assertPNG(t, ts.URL, fmt.Sprintf("/spaces/%d/og.png", sid))

	// Missing space → HTML 404.
	if resp404, _ := ogGet(t, ts.URL, "/spaces/99999"); resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("missing space: status=%d want 404", resp404.StatusCode)
	}
}
