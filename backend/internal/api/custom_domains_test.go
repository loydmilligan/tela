package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
	"github.com/zcag/tela/backend/internal/models"
)

func TestNormalizeHostname(t *testing.T) {
	cases := []struct {
		in   string
		want string // "" ⇒ expect an error
	}{
		{"wiki.example.com", "wiki.example.com"},
		{"  TELA.NGSS.IO ", "tela.ngss.io"},
		{"tela.ngss.io.", "tela.ngss.io"}, // trailing dot stripped
		{"deep.sub.example.co.uk", "deep.sub.example.co.uk"},
		{"example.com", ""},                // registrable apex — subdomain-only
		{"example.co.uk", ""},              // apex over a multi-label public suffix
		{"com", ""},                        // public suffix itself
		{"localhost", ""},                  // single label
		{"", ""},                           // empty
		{"bad_underscore.example.com", ""}, // invalid label char
		{"-lead.example.com", ""},          // leading hyphen
	}
	for _, c := range cases {
		got, err := normalizeHostname(c.in)
		if c.want == "" {
			if err == nil {
				t.Errorf("normalizeHostname(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("normalizeHostname(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

// The session↔org binding: a session is valid only on the front door it was
// created under. Exercises CreateSession (stamp) + LoadSessionAndSlide (check).
func TestSessionOrgBinding(t *testing.T) {
	d := newAPITestDB(t)
	uid := seedUser(t, d, "binduser", "password1", false)
	orgA := seedOrg(t, d, "Acme", "acme")
	orgB := seedOrg(t, d, "Beta", "beta")

	canon := context.Background()
	onA := auth.WithOrgContext(canon, auth.OrgContext{OrgID: orgA, Host: "a.example.com"})
	onB := auth.WithOrgContext(canon, auth.OrgContext{OrgID: orgB, Host: "b.example.com"})

	// Session minted on org A's custom domain.
	sidA, err := auth.CreateSession(onA, d, uid, "ua")
	if err != nil {
		t.Fatalf("create session on A: %v", err)
	}
	if _, err := auth.LoadSessionAndSlide(onA, d, sidA); err != nil {
		t.Errorf("A-session on host A: %v, want ok", err)
	}
	if _, err := auth.LoadSessionAndSlide(canon, d, sidA); !errors.Is(err, auth.ErrInvalidSession) {
		t.Errorf("A-session on canonical: %v, want ErrInvalidSession", err)
	}
	if _, err := auth.LoadSessionAndSlide(onB, d, sidA); !errors.Is(err, auth.ErrInvalidSession) {
		t.Errorf("A-session on host B: %v, want ErrInvalidSession", err)
	}

	// Session minted on the canonical host is bound to NULL — invalid on a
	// custom domain.
	sidCanon, err := auth.CreateSession(canon, d, uid, "ua")
	if err != nil {
		t.Fatalf("create session on canonical: %v", err)
	}
	if _, err := auth.LoadSessionAndSlide(canon, d, sidCanon); err != nil {
		t.Errorf("canonical session on canonical: %v, want ok", err)
	}
	if _, err := auth.LoadSessionAndSlide(onA, d, sidCanon); !errors.Is(err, auth.ErrInvalidSession) {
		t.Errorf("canonical session on host A: %v, want ErrInvalidSession", err)
	}
}

// shareOrigin derives a share URL's origin from the share's space → org →
// active hostname, independent of where the request came from.
func TestShareOriginFollowsSpaceOrg(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "shareowner", "password1", false)
	org := seedOrg(t, d, "Acme", "acme")
	space := seedSpace(t, d, "Docs", "docs", owner)
	if _, err := d.Exec(`UPDATE spaces SET org_id = $1 WHERE id = $2`, org, space); err != nil {
		t.Fatalf("set space org: %v", err)
	}

	ctx := context.Background()
	// No active hostname yet → canonical (empty in tests).
	if got := srv.shareOrigin(ctx, space); got != "" {
		t.Errorf("shareOrigin with no hostname = %q, want \"\"", got)
	}
	// Activate a hostname for the org.
	if _, err := d.Exec(
		`INSERT INTO org_hostnames (hostname, org_id, status, verify_token) VALUES ($1,$2,'active',$3)`,
		"wiki.acme.example", org, "tok"); err != nil {
		t.Fatalf("insert hostname: %v", err)
	}
	if got := srv.shareOrigin(ctx, space); got != "https://wiki.acme.example" {
		t.Errorf("shareOrigin = %q, want https://wiki.acme.example", got)
	}

	// MCP page links follow the same space → org → hostname resolution, so an
	// agent working in an org space gets org-domain links, not the canonical host.
	p := models.Page{ID: 42, SpaceID: space, Title: "Hello World"}
	if got := srv.mcpPageURL(ctx, p); got != "https://wiki.acme.example/spaces/"+strconv.FormatInt(space, 10)+"/pages/42/hello-world" {
		t.Errorf("mcpPageURL = %q, want org-domain link", got)
	}
}

// Full hostname lifecycle over HTTP: add (pending) → instance-admin force
// verify (active) → TLS ask-endpoint sees it → delete → ask-endpoint 404.
func TestOrgHostnameLifecycle(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "admin", "adminpass1", true)
	org := seedOrg(t, d, "Acme", "acme")
	c := loginClient(t, ts, "admin", "adminpass1")

	// Add.
	var added struct {
		Hostname orgHostnameDTO `json:"hostname"`
	}
	cdPost(t, c, ts.URL+"/api/orgs/"+itoa(org)+"/hostnames", `{"hostname":"wiki.example.com"}`, http.StatusCreated, &added)
	if added.Hostname.Status != "pending" || added.Hostname.TXTName != "_tela-verify.wiki.example.com" || added.Hostname.TXTValue == "" {
		t.Fatalf("unexpected add response: %+v", added.Hostname)
	}

	// Not yet active → TLS ask-endpoint says no.
	if code := getStatus(t, c, ts.URL+"/api/internal/tls-check?domain=wiki.example.com"); code != http.StatusNotFound {
		t.Fatalf("tls-check before verify = %d, want 404", code)
	}

	// Instance admin force-verifies (skips DNS).
	var verified struct {
		Hostname orgHostnameDTO `json:"hostname"`
	}
	cdPost(t, c, ts.URL+"/api/orgs/"+itoa(org)+"/hostnames/wiki.example.com/verify", ``, http.StatusOK, &verified)
	if verified.Hostname.Status != "active" || verified.Hostname.VerifiedAt == nil {
		t.Fatalf("verify did not activate: %+v", verified.Hostname)
	}

	// Active → ask-endpoint 200; unknown host → 404.
	if code := getStatus(t, c, ts.URL+"/api/internal/tls-check?domain=wiki.example.com"); code != http.StatusOK {
		t.Fatalf("tls-check after verify = %d, want 200", code)
	}
	if code := getStatus(t, c, ts.URL+"/api/internal/tls-check?domain=nope.example.com"); code != http.StatusNotFound {
		t.Fatalf("tls-check unknown = %d, want 404", code)
	}

	// Delete → ask-endpoint 404 again.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/orgs/"+itoa(org)+"/hostnames/wiki.example.com", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", resp.StatusCode)
	}
	if code := getStatus(t, c, ts.URL+"/api/internal/tls-check?domain=wiki.example.com"); code != http.StatusNotFound {
		t.Fatalf("tls-check after delete = %d, want 404", code)
	}
}

func TestOrgHostnameValidation(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "admin", "adminpass1", true)
	org := seedOrg(t, d, "Acme", "acme")
	c := loginClient(t, ts, "admin", "adminpass1")
	base := ts.URL + "/api/orgs/" + itoa(org) + "/hostnames"

	// Registrable apex is rejected (subdomain-only).
	cdPost(t, c, base, `{"hostname":"example.com"}`, http.StatusBadRequest, nil)

	// First registration succeeds; the same host again 409s.
	cdPost(t, c, base, `{"hostname":"docs.example.com"}`, http.StatusCreated, nil)
	cdPost(t, c, base, `{"hostname":"docs.example.com"}`, http.StatusConflict, nil)
}

// Host-context drives login-screen branding + which sign-in methods show, and
// the password toggle is enforced server-side on the org's domain.
func TestHostContextAndLoginSettings(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "admin", "adminpass1", true)
	seedUser(t, d, "member", "memberpass1", false)
	org := seedOrg(t, d, "Acme", "acme")
	if _, err := d.Exec(
		`INSERT INTO org_hostnames (hostname, org_id, status, verify_token) VALUES ($1,$2,'active',$3)`,
		"wiki.example.com", org, "tok"); err != nil {
		t.Fatalf("seed hostname: %v", err)
	}
	admin := loginClient(t, ts, "admin", "adminpass1")

	// On the custom host: org present, both methods enabled by default.
	var hc hostContextDTO
	getHost(t, http.DefaultClient, ts.URL+"/api/host-context", "wiki.example.com", http.StatusOK, &hc)
	if hc.Org == nil || hc.Org.Name != "Acme" || !hc.Login.PasswordEnabled || !hc.Login.SocialEnabled {
		t.Fatalf("custom-host context = %+v", hc)
	}
	// On the canonical host: no org.
	var hc2 hostContextDTO
	getHost(t, http.DefaultClient, ts.URL+"/api/host-context", "", http.StatusOK, &hc2)
	if hc2.Org != nil {
		t.Fatalf("canonical host context should have no org: %+v", hc2)
	}

	// Disable password sign-in for the org.
	cdPut(t, admin, ts.URL+"/api/orgs/"+itoa(org)+"/login-settings",
		`{"password_enabled":false,"social_enabled":true}`, http.StatusOK)

	getHost(t, http.DefaultClient, ts.URL+"/api/host-context", "wiki.example.com", http.StatusOK, &hc)
	if hc.Login.PasswordEnabled {
		t.Fatalf("password should be disabled after PUT: %+v", hc.Login)
	}

	// Server-side enforcement: password login on the org's host is refused.
	code := postHostStatus(t, ts.URL+"/api/auth/login", "wiki.example.com",
		`{"username":"member","password":"memberpass1"}`)
	if code != http.StatusForbidden {
		t.Fatalf("password login on disabled host = %d, want 403", code)
	}
	// ...but it still works on the canonical host.
	code = postHostStatus(t, ts.URL+"/api/auth/login", "", `{"username":"member","password":"memberpass1"}`)
	if code != http.StatusOK {
		t.Fatalf("password login on canonical = %d, want 200", code)
	}

	// Can't disable every method without SSO configured.
	cdPut(t, admin, ts.URL+"/api/orgs/"+itoa(org)+"/login-settings",
		`{"password_enabled":false,"social_enabled":false}`, http.StatusBadRequest)
}

// Org branding is validated and surfaced through host-context.
func TestOrgBranding(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "admin", "adminpass1", true)
	org := seedOrg(t, d, "Acme", "acme")
	if _, err := d.Exec(
		`INSERT INTO org_hostnames (hostname, org_id, status, verify_token) VALUES ($1,$2,'active',$3)`,
		"wiki.example.com", org, "tok"); err != nil {
		t.Fatalf("seed hostname: %v", err)
	}
	admin := loginClient(t, ts, "admin", "adminpass1")
	base := ts.URL + "/api/orgs/" + itoa(org) + "/branding"

	// Reject a CSS-injection-shaped accent and a non-http(s) logo import URL.
	cdPut(t, admin, base, `{"accent":"red;}body{}"}`, http.StatusBadRequest)
	cdPut(t, admin, base, `{"logo_import_url":"ftp://acme.example/logo.png","accent":"#ff0000"}`, http.StatusBadRequest)

	// Set the accent via PUT (the logo is uploaded separately, below).
	cdPut(t, admin, base, `{"accent":"oklch(0.7 0.1 250)"}`, http.StatusOK)

	// Upload a logo — stored IN tela; host-context exposes a tela serve route, not
	// an external URL, so the deck renderer can always reach it.
	req, _ := http.NewRequest(http.MethodPost, base+"/logo", bytes.NewReader(tinyPNG))
	req.Header.Set("Content-Type", "image/png")
	resp, err := admin.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("upload logo: err=%v status=%d", err, resp.StatusCode)
	}
	resp.Body.Close()

	var hc hostContextDTO
	getHost(t, http.DefaultClient, ts.URL+"/api/host-context", "wiki.example.com", http.StatusOK, &hc)
	wantPrefix := "/api/public/orgs/" + itoa(org) + "/logo"
	if hc.Org == nil || !strings.HasPrefix(hc.Org.LogoURL, wantPrefix) || hc.Org.Accent != "oklch(0.7 0.1 250)" {
		t.Fatalf("host-context branding = %+v (want logo prefix %q)", hc.Org, wantPrefix)
	}
	// The advertised route serves the logo bytes publicly.
	lr, err := http.Get(ts.URL + hc.Org.LogoURL)
	if err != nil || lr.StatusCode != http.StatusOK || lr.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("serve logo: err=%v status=%d type=%q", err, lr.StatusCode, lr.Header.Get("Content-Type"))
	}
	lr.Body.Close()
}

// The health probe is gated to the org's own hostnames (no DNS call on the 404
// path — keeps the test hermetic).
func TestOrgHostnameHealthGate(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "admin", "adminpass1", true)
	org := seedOrg(t, d, "Acme", "acme")
	c := loginClient(t, ts, "admin", "adminpass1")
	if code := getStatus(t, c, ts.URL+"/api/orgs/"+itoa(org)+"/hostnames/notmine.example.com/health"); code != http.StatusNotFound {
		t.Fatalf("health for non-org hostname = %d, want 404", code)
	}
}

// --- small test helpers (itoa lives in attachments_test.go) ---

func cdPut(t *testing.T, c *http.Client, url, body string, wantStatus int) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("PUT %s = %d, want %d", url, resp.StatusCode, wantStatus)
	}
}

func getHost(t *testing.T, c *http.Client, url, host string, wantStatus int, out any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if host != "" {
		req.Host = host
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s (host %q): %v", url, host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s (host %q) = %d, want %d", url, host, resp.StatusCode, wantStatus)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
}

func postHostStatus(t *testing.T, url, host, body string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s (host %q): %v", url, host, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func cdPost(t *testing.T, c *http.Client, url, body string, wantStatus int, out any) {
	t.Helper()
	resp, err := c.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("POST %s = %d, want %d", url, resp.StatusCode, wantStatus)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
}

func getStatus(t *testing.T, c *http.Client, url string) int {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}
