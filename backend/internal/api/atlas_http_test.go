package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// atlasReq runs an authed JSON request and returns (status, body).
func atlasReq(t *testing.T, c *http.Client, method, url, body string) (int, string) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// TestAtlasSources_GatingAndCRUD locks the access model + the source CRUD surface:
// management (create/run/delete) is owner/org-admin only — an EDITOR is denied —
// while viewing (list) is open to any member; and triggering a run when the AI
// backends are unconfigured returns the 503 enablement guard (not a 500).
func TestAtlasSources_GatingAndCRUD(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)   // space owner
	seedUser(t, d, "bob", "bobpw1234", false)              // non-member
	charlie := seedUser(t, d, "charlie", "charliepw1", false) // editor member
	spaceA := seedSpace(t, d, "Repo Docs", "repo-docs", alice)
	seedMember(t, d, spaceA, charlie, "editor")

	ca := loginClient(t, ts, "alice", "alicepw12")
	cb := loginClient(t, ts, "bob", "bobpw1234")
	cc := loginClient(t, ts, "charlie", "charliepw1")

	sources := fmt.Sprintf("%s/api/spaces/%d/atlas/sources", ts.URL, spaceA)
	body := `{"type":"git","location":"https://github.com/example/repo.git","name":"repo"}`

	// Owner can bind a source.
	st, resp := atlasReq(t, ca, "POST", sources, body)
	if st != http.StatusCreated {
		t.Fatalf("owner create: status=%d body=%s", st, resp)
	}
	var created struct {
		Source struct {
			ID int64 `json:"id"`
		} `json:"source"`
	}
	if err := json.Unmarshal([]byte(resp), &created); err != nil || created.Source.ID == 0 {
		t.Fatalf("decode created source: err=%v body=%s", err, resp)
	}
	srcID := created.Source.ID

	// Editor is denied management (admin-level on purpose); non-member too.
	if st, _ := atlasReq(t, cc, "POST", sources, body); st != http.StatusForbidden {
		t.Fatalf("editor create: want 403, got %d", st)
	}
	if st, _ := atlasReq(t, cb, "POST", sources, body); st != http.StatusForbidden {
		t.Fatalf("non-member create: want 403, got %d", st)
	}

	// Any member can view; non-member cannot.
	if st, lb := atlasReq(t, cc, "GET", sources, ""); st != http.StatusOK {
		t.Fatalf("editor list: want 200, got %d %s", st, lb)
	}
	if st, _ := atlasReq(t, cb, "GET", sources, ""); st != http.StatusForbidden {
		t.Fatalf("non-member list: want 403, got %d", st)
	}
	st, lb := atlasReq(t, ca, "GET", sources, "")
	if st != http.StatusOK || !strings.Contains(lb, `"managed":true`) ||
		!strings.Contains(lb, "github.com/example/repo.git") {
		t.Fatalf("owner list: status=%d body=%s", st, lb)
	}

	// Trigger a run: gating passes for the owner, then the enablement guard fires
	// (no embedder / LLM configured in tests) — a clean 503, not a crash.
	runURL := fmt.Sprintf("%s/api/atlas/sources/%d/run", ts.URL, srcID)
	if st, rb := atlasReq(t, ca, "POST", runURL, ""); st != http.StatusServiceUnavailable || !strings.Contains(rb, "ai_unavailable") {
		t.Fatalf("owner run: want 503 ai_unavailable, got %d %s", st, rb)
	}
	// Editor can't trigger (management gate fires before the enablement guard).
	if st, _ := atlasReq(t, cc, "POST", runURL, ""); st != http.StatusForbidden {
		t.Fatalf("editor run: want 403, got %d", st)
	}

	// Unknown run → 404.
	if st, _ := atlasReq(t, ca, "GET", fmt.Sprintf("%s/api/atlas/runs/999999", ts.URL), ""); st != http.StatusNotFound {
		t.Fatalf("missing run: want 404, got %d", st)
	}

	// Editor can't delete; owner can; the space then reads as unmanaged.
	delURL := fmt.Sprintf("%s/api/atlas/sources/%d", ts.URL, srcID)
	if st, _ := atlasReq(t, cc, "DELETE", delURL, ""); st != http.StatusForbidden {
		t.Fatalf("editor delete: want 403, got %d", st)
	}
	if st, _ := atlasReq(t, ca, "DELETE", delURL, ""); st != http.StatusNoContent {
		t.Fatalf("owner delete: want 204, got %d", st)
	}
	if st, b := atlasReq(t, ca, "GET", sources, ""); st != http.StatusOK || !strings.Contains(b, `"managed":false`) {
		t.Fatalf("after delete: status=%d body=%s", st, b)
	}
}
