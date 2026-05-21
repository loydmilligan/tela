package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/models"
)

// postImportMira shapes a JSON POST to /api/spaces/{id}/import-mira using the
// client + path that newWiredServer hands back. body is the raw JSON literal
// so each test can pin field-presence exactly (json.Marshal of a partially
// populated struct would erase the "neither set" / "both set" branches the
// handler distinguishes).
func postImportMira(t *testing.T, c *http.Client, baseURL string, spaceID int64, body string) (*http.Response, []byte) {
	t.Helper()
	resp, err := c.Post(
		fmt.Sprintf("%s/api/spaces/%d/import-mira", baseURL, spaceID),
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("post import-mira: %v", err)
	}
	out, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, out
}

type importMiraResponse struct {
	Page models.Page `json:"page"`
}

func decodeImportMiraResp(t *testing.T, body []byte) importMiraResponse {
	t.Helper()
	var got importMiraResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode import-mira response: %v (body=%s)", err, body)
	}
	return got
}

// TestImportMira_FullFlow exercises every documented branch of the
// import-mira endpoint against the wired server, with one fixture per test
// family. The shared admin/viewer users + space mirror the markdown-import
// fixture so role gating regressions light up the same way.
func TestImportMira_FullFlow(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	space := seedSpace(t, d, "Test Space", "test-space", admin)
	seedMember(t, d, space, bob, roleViewer)

	adminC := loginClient(t, ts, "admin", "adminpw12")
	bobC := loginClient(t, ts, "bob", "bobpw1234")

	t.Run("payload_happy_path", func(t *testing.T) {
		payload := `{
			"template":"page",
			"blocks":[
				{"type":"heading_1","heading_1":{"rich_text":[{"type":"text","text":{"content":"My Imported Page"}}]}},
				{"type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"hello from mira"}}]}}
			]
		}`
		body := fmt.Sprintf(`{"payload": %s}`, payload)
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status=%d body=%s want 201", resp.StatusCode, out)
		}
		got := decodeImportMiraResp(t, out)
		if got.Page.Title != "My Imported Page" {
			t.Fatalf("title=%q want %q", got.Page.Title, "My Imported Page")
		}
		if got.Page.SpaceID != space {
			t.Fatalf("space_id=%d want %d", got.Page.SpaceID, space)
		}
		if got.Page.ParentID != nil {
			t.Fatalf("parent_id=%v want nil", got.Page.ParentID)
		}
		if !strings.Contains(got.Page.Body, "hello from mira") {
			t.Fatalf("body missing rendered paragraph: %q", got.Page.Body)
		}
		// Payload import → no source comment.
		if strings.Contains(got.Page.Body, "mira-source:") {
			t.Fatalf("payload import leaked source comment: %q", got.Page.Body)
		}
		var dbBody string
		if err := d.QueryRow(`SELECT body FROM pages WHERE id = ?`, got.Page.ID).Scan(&dbBody); err != nil {
			t.Fatalf("query body: %v", err)
		}
		if !strings.Contains(dbBody, "hello from mira") {
			t.Fatalf("db body missing content: %q", dbBody)
		}
	})

	t.Run("url_https_only", func(t *testing.T) {
		t.Setenv("TELA_MIRA_ALLOWED_HOSTS", "mira.cagdas.io")
		body := `{"source_url": "http://mira.cagdas.io/p/foo"}`
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(out), `"code":"bad_request"`) {
			t.Fatalf("status=%d body=%s want 400 bad_request", resp.StatusCode, out)
		}
	})

	t.Run("url_host_not_allowed", func(t *testing.T) {
		t.Setenv("TELA_MIRA_ALLOWED_HOSTS", "mira.cagdas.io")
		body := `{"source_url": "https://evil.example.com/p/foo"}`
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(out), `"code":"forbidden"`) {
			t.Fatalf("status=%d body=%s want 403 forbidden", resp.StatusCode, out)
		}
	})

	t.Run("url_empty_allowlist_fails_closed", func(t *testing.T) {
		// Explicit empty string env → no allowed hosts. Even the default host
		// should be rejected.
		t.Setenv("TELA_MIRA_ALLOWED_HOSTS", "")
		body := `{"source_url": "https://mira.cagdas.io/p/foo"}`
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(out), `"code":"forbidden"`) {
			t.Fatalf("status=%d body=%s want 403 forbidden", resp.StatusCode, out)
		}
	})

	t.Run("url_non_json_content_type", func(t *testing.T) {
		mira := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, "<html>not json</html>")
		}))
		defer mira.Close()
		host := mustHost(t, mira.URL)
		t.Setenv("TELA_MIRA_ALLOWED_HOSTS", host)
		trustTestTLS(t)
		body := fmt.Sprintf(`{"source_url": %q}`, mira.URL)
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusUnsupportedMediaType || !strings.Contains(string(out), `"code":"bad_request"`) {
			t.Fatalf("status=%d body=%s want 415 bad_request", resp.StatusCode, out)
		}
	})

	t.Run("url_redirect_rejected", func(t *testing.T) {
		// Defense-in-depth: a 30x from an allowlisted host MUST NOT be followed,
		// since the redirect target's host isn't re-checked against the
		// allowlist. fetchMiraSource sets CheckRedirect → ErrUseLastResponse so
		// the redirect surfaces as a non-2xx, which the handler maps to 400.
		mira := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "https://internal.private:8443/secret")
			w.WriteHeader(http.StatusFound)
		}))
		defer mira.Close()
		host := mustHost(t, mira.URL)
		t.Setenv("TELA_MIRA_ALLOWED_HOSTS", host)
		trustTestTLS(t)
		body := fmt.Sprintf(`{"source_url": %q}`, mira.URL)
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(out), `"code":"bad_request"`) {
			t.Fatalf("status=%d body=%s want 400 bad_request", resp.StatusCode, out)
		}
	})

	t.Run("url_json_path_auto_appended", func(t *testing.T) {
		// PO pastes the canonical mira render URL `/p/<slug>` (text/html); the
		// backend should transparently fetch the `/p/<slug>.json` alternate so
		// every entry point (FE Settings, paste-hook, MCP, raw curl) works
		// without each layer having to learn the rewrite rule.
		var gotPath string
		mira := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			if r.URL.Path != "/p/cagdas-brief.json" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{
				"template":"page",
				"blocks":[
					{"type":"heading_1","heading_1":{"rich_text":[{"type":"text","text":{"content":"Brief"}}]}}
				]
			}`)
		}))
		defer mira.Close()
		host := mustHost(t, mira.URL)
		t.Setenv("TELA_MIRA_ALLOWED_HOSTS", host)
		trustTestTLS(t)
		bareURL := mira.URL + "/p/cagdas-brief"
		body := fmt.Sprintf(`{"source_url": %q}`, bareURL)
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status=%d body=%s want 201 (auto-append should rewrite /p/<slug> → /p/<slug>.json)", resp.StatusCode, out)
		}
		if gotPath != "/p/cagdas-brief.json" {
			t.Fatalf("upstream got path=%q want /p/cagdas-brief.json", gotPath)
		}
		got := decodeImportMiraResp(t, out)
		// Source comment preserves the PO-supplied URL, NOT the rewritten one
		// — so deletion + re-import via the comment marker can round-trip.
		wantComment := fmt.Sprintf("<!-- mira-source: %s -->", bareURL)
		if !strings.Contains(got.Page.Body, wantComment) {
			t.Fatalf("body missing original source comment %q: %q", wantComment, got.Page.Body)
		}
	})

	t.Run("url_json_path_no_rewrite_outside_slug_shape", func(t *testing.T) {
		// Defense-in-depth: paths that aren't a bare slug (already-suffixed,
		// nested, or non-/p/ shapes) must pass through unchanged so the
		// content-type guard still fails closed on misuse.
		mira := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Serve only the verbatim path; .json rewrite would produce a 404
			// here, which we'd then fail the test on.
			if r.URL.Path != "/r/sometoken" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"template":"page","blocks":[]}`)
		}))
		defer mira.Close()
		host := mustHost(t, mira.URL)
		t.Setenv("TELA_MIRA_ALLOWED_HOSTS", host)
		trustTestTLS(t)
		body := fmt.Sprintf(`{"source_url": "%s/r/sometoken"}`, mira.URL)
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status=%d body=%s want 201 (non-slug path should NOT be rewritten)", resp.StatusCode, out)
		}
	})

	t.Run("url_password_required", func(t *testing.T) {
		// Mira's password-gated pages return a 401 with a useful
		// {error, unlock} JSON envelope. The handler must surface this as a
		// distinct 403 with the unlock URL preserved so clients can guide
		// the user to the unlock page instead of swallowing it as a
		// generic non-2xx.
		const unlockURL = "https://mira.cagdas.io/r/abc123token"
		mira := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":"password_required","unlock":%q}`, unlockURL)
		}))
		defer mira.Close()
		host := mustHost(t, mira.URL)
		t.Setenv("TELA_MIRA_ALLOWED_HOSTS", host)
		trustTestTLS(t)
		body := fmt.Sprintf(`{"source_url": %q}`, mira.URL)
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status=%d body=%s want 403", resp.StatusCode, out)
		}
		var env struct {
			Error  string `json:"error"`
			Code   string `json:"code"`
			Unlock string `json:"unlock"`
		}
		if err := json.Unmarshal(out, &env); err != nil {
			t.Fatalf("decode error envelope: %v (body=%s)", err, out)
		}
		if env.Code != "mira_password_required" {
			t.Fatalf("code=%q want mira_password_required (body=%s)", env.Code, out)
		}
		if env.Unlock != unlockURL {
			t.Fatalf("unlock=%q want %q", env.Unlock, unlockURL)
		}
		if env.Error == "" {
			t.Fatalf("error message must be non-empty (body=%s)", out)
		}
	})

	t.Run("url_oversized_response", func(t *testing.T) {
		big := strings.Repeat("a", (1<<20)+10)
		mira := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"template":"page","blocks":[{"type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"%s"}}]}}]}`, big)
		}))
		defer mira.Close()
		host := mustHost(t, mira.URL)
		t.Setenv("TELA_MIRA_ALLOWED_HOSTS", host)
		trustTestTLS(t)
		body := fmt.Sprintf(`{"source_url": %q}`, mira.URL)
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(string(out), `"code":"bad_request"`) {
			t.Fatalf("status=%d body=%s want 413 bad_request", resp.StatusCode, out)
		}
	})

	t.Run("oversized_payload", func(t *testing.T) {
		// Build a JSON request body that exceeds the 1 MiB cap. The handler
		// applies MaxBytesReader on the request body itself, so this fires
		// before the payload exclusivity check.
		big := strings.Repeat("a", (1<<20)+100)
		body := fmt.Sprintf(`{"payload": {"template":"page","blocks":[{"type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"%s"}}]}}]}}`, big)
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(string(out), `"code":"bad_request"`) {
			t.Fatalf("status=%d body=%s want 413 bad_request", resp.StatusCode, out)
		}
	})

	t.Run("neither_set", func(t *testing.T) {
		resp, out := postImportMira(t, adminC, ts.URL, space, `{}`)
		if resp.StatusCode != http.StatusBadRequest ||
			!strings.Contains(string(out), `"code":"bad_request"`) ||
			!strings.Contains(string(out), "exactly one of source_url or payload") {
			t.Fatalf("status=%d body=%s want 400 bad_request", resp.StatusCode, out)
		}
	})

	t.Run("both_set", func(t *testing.T) {
		body := `{"source_url": "https://mira.cagdas.io/p/foo", "payload": {"template":"page","blocks":[]}}`
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusBadRequest ||
			!strings.Contains(string(out), `"code":"bad_request"`) ||
			!strings.Contains(string(out), "exactly one of source_url or payload") {
			t.Fatalf("status=%d body=%s want 400 bad_request", resp.StatusCode, out)
		}
	})

	t.Run("invalid_mira_json", func(t *testing.T) {
		// Valid JSON envelope around the payload field, but the payload value
		// itself isn't a valid mira page (string where object expected).
		body := `{"payload": "not a mira page object"}`
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(out), `"code":"bad_request"`) {
			t.Fatalf("status=%d body=%s want 400 bad_request", resp.StatusCode, out)
		}
	})

	t.Run("viewer_forbidden", func(t *testing.T) {
		body := `{"payload": {"template":"page","blocks":[{"type":"heading_1","heading_1":{"rich_text":[{"type":"text","text":{"content":"X"}}]}}]}}`
		resp, out := postImportMira(t, bobC, ts.URL, space, body)
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(out), `"code":"forbidden"`) {
			t.Fatalf("status=%d body=%s want 403 forbidden", resp.StatusCode, out)
		}
	})

	t.Run("space_not_found", func(t *testing.T) {
		body := `{"payload": {"template":"page","blocks":[]}}`
		resp, out := postImportMira(t, adminC, ts.URL, 999999, body)
		if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(out), `"code":"space_not_found"`) {
			t.Fatalf("status=%d body=%s want 404 space_not_found", resp.StatusCode, out)
		}
	})

	t.Run("parent_id_in_target_space", func(t *testing.T) {
		res, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
		                    VALUES (?, NULL, 'Parent', '', 0)`, space)
		if err != nil {
			t.Fatalf("seed parent: %v", err)
		}
		parentID, _ := res.LastInsertId()
		body := fmt.Sprintf(`{"parent_id": %d, "payload": {"template":"page","blocks":[{"type":"heading_1","heading_1":{"rich_text":[{"type":"text","text":{"content":"Child"}}]}}]}}`, parentID)
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status=%d body=%s want 201", resp.StatusCode, out)
		}
		got := decodeImportMiraResp(t, out)
		if got.Page.ParentID == nil || *got.Page.ParentID != parentID {
			t.Fatalf("parent_id=%v want %d", got.Page.ParentID, parentID)
		}
	})

	t.Run("parent_id_in_other_space", func(t *testing.T) {
		otherSpace := seedSpace(t, d, "Other", "other", admin)
		res, err := d.Exec(`INSERT INTO pages (space_id, parent_id, title, body, position)
		                    VALUES (?, NULL, 'X', '', 0)`, otherSpace)
		if err != nil {
			t.Fatalf("seed cross-space: %v", err)
		}
		otherID, _ := res.LastInsertId()
		body := fmt.Sprintf(`{"parent_id": %d, "payload": {"template":"page","blocks":[]}}`, otherID)
		resp, out := postImportMira(t, adminC, ts.URL, space, body)
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(out), `"code":"bad_request"`) {
			t.Fatalf("status=%d body=%s want 400 bad_request", resp.StatusCode, out)
		}
	})
}

// TestImportMira_URLHappyPath drives the URL-fetch branch end-to-end. The
// production handler uses a default-transport http.Client which can't reach
// httptest.NewTLSServer's self-signed cert, so the test patches
// DefaultTransport's TLSClientConfig for the test's lifetime.
func TestImportMira_URLHappyPath(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "Test Space", "test-space", admin)
	adminC := loginClient(t, ts, "admin", "adminpw12")

	mira := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		io.WriteString(w, `{
			"template":"page",
			"blocks":[
				{"type":"heading_1","heading_1":{"rich_text":[{"type":"text","text":{"content":"URL Page"}}]}},
				{"type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"fetched body"}}]}}
			]
		}`)
	}))
	defer mira.Close()
	host := mustHost(t, mira.URL)
	t.Setenv("TELA_MIRA_ALLOWED_HOSTS", host)
	trustTestTLS(t)

	body := fmt.Sprintf(`{"source_url": %q}`, mira.URL)
	resp, out := postImportMira(t, adminC, ts.URL, space, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d body=%s want 201", resp.StatusCode, out)
	}
	got := decodeImportMiraResp(t, out)
	if got.Page.Title != "URL Page" {
		t.Fatalf("title=%q want %q", got.Page.Title, "URL Page")
	}
	if !strings.Contains(got.Page.Body, "fetched body") {
		t.Fatalf("body missing rendered paragraph: %q", got.Page.Body)
	}
	wantComment := fmt.Sprintf("<!-- mira-source: %s -->", mira.URL)
	if !strings.Contains(got.Page.Body, wantComment) {
		t.Fatalf("body missing source comment %q: %q", wantComment, got.Page.Body)
	}
	if !strings.HasSuffix(got.Page.Body, "-->\n") {
		t.Fatalf("body should end with source-comment line + newline; got tail %q", lastN(got.Page.Body, 40))
	}
}

// mustHost extracts the lowercase hostname from a URL string for use as an
// allowlist entry. httptest URLs are 127.0.0.1, which is a valid allowlist
// host token.
func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return strings.ToLower(u.Hostname())
}

// trustTestTLS patches http.DefaultTransport's TLS config to skip cert
// verification for the duration of the test, so fetchMiraSource (which uses
// &http.Client{Timeout:...} with the default transport) can reach
// httptest.NewTLSServer. Restored on Cleanup.
//
// Adding a code seam to ImportMira purely for tests would muddy the production
// surface; InsecureSkipVerify is localized here, scoped to the test, and
// reverted automatically.
func trustTestTLS(t *testing.T) {
	t.Helper()
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport is not *http.Transport (%T)", http.DefaultTransport)
	}
	prev := tr.TLSClientConfig
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	t.Cleanup(func() { tr.TLSClientConfig = prev })
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
