package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// A tiny byte string whose PNG signature makes http.DetectContentType report
// image/png (enough to exercise storage/validation without a real encoder).
var tinyPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDRtiny-logo-bytes")

// storeOrgLogo stores raster bytes in tela, points logo_url at the content-
// addressed serve route, and that route serves the bytes publicly; clearing it
// removes the asset.
func TestOrgLogo_StoreServeClear(t *testing.T) {
	ts, d, srv := newWiredServerOnDiskWithSrv(t)
	ctx := context.Background()
	org := seedOrg(t, d, "Acme", "acme")

	if ae := srv.storeOrgLogo(ctx, org, tinyPNG); ae != nil {
		t.Fatalf("storeOrgLogo: %+v", ae)
	}

	// logo_url is the tela serve route (not an external URL); orgBranding returns it.
	logoURL, _ := srv.orgBranding(ctx, org)
	want := fmt.Sprintf("/api/public/orgs/%d/logo", org)
	if !strings.HasPrefix(logoURL, want) {
		t.Fatalf("logo_url = %q, want prefix %q", logoURL, want)
	}
	var mime, hash string
	if err := d.QueryRow(`SELECT logo_mime, logo_hash FROM org_branding WHERE org_id=$1`, org).Scan(&mime, &hash); err != nil {
		t.Fatal(err)
	}
	if mime != "image/png" || hash == "" {
		t.Fatalf("stored mime=%q hash=%q", mime, hash)
	}

	// The public serve route returns the bytes (no auth) with the right type + ETag.
	resp, err := http.Get(ts.URL + want)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("serve: status=%d type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if got := resp.Header.Get("ETag"); got != `"`+hash+`"` {
		t.Errorf("etag = %q, want %q", got, hash)
	}

	// If-None-Match on the current hash → 304.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+want, nil)
	req.Header.Set("If-None-Match", `"`+hash+`"`)
	r2, _ := http.DefaultClient.Do(req)
	if r2.StatusCode != http.StatusNotModified {
		t.Errorf("If-None-Match → %d, want 304", r2.StatusCode)
	}
	r2.Body.Close()

	// Clear → the asset is gone, serve 404, has_logo false.
	if _, err := d.ExecContext(ctx,
		`UPDATE org_branding SET logo_data=NULL, logo_mime='', logo_hash='', logo_url='' WHERE org_id=$1`, org); err != nil {
		t.Fatal(err)
	}
	r3, _ := http.Get(ts.URL + want)
	if r3.StatusCode != http.StatusNotFound {
		t.Errorf("after clear, serve → %d, want 404", r3.StatusCode)
	}
	r3.Body.Close()
}

// storeOrgLogo rejects non-image bytes; importOrgLogo rejects a non-http URL.
func TestOrgLogo_Validation(t *testing.T) {
	_, d, srv := newWiredServerOnDiskWithSrv(t)
	ctx := context.Background()
	org := seedOrg(t, d, "Acme", "acme")

	if ae := srv.storeOrgLogo(ctx, org, []byte("not an image at all")); ae == nil || ae.Status != http.StatusBadRequest {
		t.Fatalf("non-image should be 400, got %+v", ae)
	}
	if ae := srv.importOrgLogo(ctx, org, "ftp://example.com/logo.png"); ae == nil || ae.Status != http.StatusBadRequest {
		t.Fatalf("non-http import URL should be 400, got %+v", ae)
	}
}
