package api

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestExportPageMarkdown(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "S", "s", admin)
	c := loginClient(t, ts, "admin", "adminpw12")

	resp, err := postJSON(c, ts.URL+"/api/pages",
		fmt.Sprintf(`{"space_id":%d,"title":"My Page","body":"hello body","props":{"owner":"cagdas"}}`, space))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: err=%v status=%d", err, resp.StatusCode)
	}
	page := decodePage(t, resp)

	mdResp, err := c.Get(fmt.Sprintf("%s/api/pages/%d/md", ts.URL, page.ID))
	if err != nil {
		t.Fatalf("get md: %v", err)
	}
	defer mdResp.Body.Close()
	if mdResp.StatusCode != http.StatusOK {
		t.Fatalf("get md: status=%d", mdResp.StatusCode)
	}
	if ct := mdResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("content-type=%q", ct)
	}
	if cd := mdResp.Header.Get("Content-Disposition"); !strings.Contains(cd, "my-page.md") {
		t.Fatalf("content-disposition=%q want my-page.md", cd)
	}
	body, _ := io.ReadAll(mdResp.Body)
	md := string(body)
	for _, want := range []string{"---\n", "title: My Page", "slug: my-page", "owner: cagdas", "hello body"} {
		if !strings.Contains(md, want) {
			t.Fatalf("md missing %q:\n%s", want, md)
		}
	}
}

func TestExportSpaceMarkdownZip(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "My Space", "my-space", admin)
	c := loginClient(t, ts, "admin", "adminpw12")

	mk := func(title, body string, parent *int64) int64 {
		t.Helper()
		payload := fmt.Sprintf(`{"space_id":%d,"title":%q,"body":%q}`, space, title, body)
		if parent != nil {
			payload = fmt.Sprintf(`{"space_id":%d,"parent_id":%d,"title":%q,"body":%q}`, space, *parent, title, body)
		}
		resp, err := postJSON(c, ts.URL+"/api/pages", payload)
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("create %q: err=%v status=%d", title, err, resp.StatusCode)
		}
		return decodePage(t, resp).ID
	}
	parent := mk("Engineering", "eng root", nil)
	mk("RFC One", "the rfc", &parent)

	resp, err := c.Get(fmt.Sprintf("%s/api/spaces/%d/export.zip", ts.URL, space))
	if err != nil {
		t.Fatalf("get zip: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get zip: status=%d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("content-type=%q", ct)
	}
	raw, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(b)
	}
	// Parent → engineering.md; child nests under engineering/.
	if _, ok := files["engineering.md"]; !ok {
		t.Fatalf("missing engineering.md; got %v", keys(files))
	}
	child, ok := files["engineering/rfc-one.md"]
	if !ok {
		t.Fatalf("missing engineering/rfc-one.md; got %v", keys(files))
	}
	if !strings.Contains(child, "title: RFC One") || !strings.Contains(child, "the rfc") {
		t.Fatalf("child md content wrong:\n%s", child)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
