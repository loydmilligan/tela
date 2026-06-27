package api

import (
	"context"
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

// TestAtlasSources_GatingAndCRUD locks the source surface under a project:
// management (create/run/delete) is owner-only — a non-owner is denied — while
// viewing the sources list is open to the owner; triggering a run when the AI
// backends are unconfigured returns the 503 enablement guard (not a 500).
func TestAtlasSources_GatingAndCRUD(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false) // project owner
	seedUser(t, d, "bob", "bobpw1234", false)            // stranger
	ca := loginClient(t, ts, "alice", "alicepw12")
	cb := loginClient(t, ts, "bob", "bobpw1234")

	space := seedSpace(t, d, "Repo Docs", "repo-docs", alice)
	pid := seedAtlasProject(t, d, "Repo Docs", accountUser, alice, space, 0)

	sources := fmt.Sprintf("%s/api/atlas/projects/%d/sources", ts.URL, pid)
	body := `{"type":"git","location":"https://github.com/example/repo.git","name":"repo"}`

	// Owner can add a source.
	st, resp := atlasReq(t, ca, "POST", sources, body)
	if st != http.StatusCreated {
		t.Fatalf("owner create: status=%d body=%s", st, resp)
	}
	var created struct {
		Source struct {
			ID        int64 `json:"id"`
			ProjectID int64 `json:"project_id"`
		} `json:"source"`
	}
	if err := json.Unmarshal([]byte(resp), &created); err != nil || created.Source.ID == 0 {
		t.Fatalf("decode created source: err=%v body=%s", err, resp)
	}
	if created.Source.ProjectID != pid {
		t.Fatalf("source project_id = %d, want %d", created.Source.ProjectID, pid)
	}
	srcID := created.Source.ID

	// Stranger is denied management + view.
	if st, _ := atlasReq(t, cb, "POST", sources, body); st != http.StatusForbidden {
		t.Fatalf("stranger create: want 403, got %d", st)
	}
	if st, _ := atlasReq(t, cb, "GET", sources, ""); st != http.StatusForbidden {
		t.Fatalf("stranger list: want 403, got %d", st)
	}

	// Owner can view; the bound source shows up.
	st, lb := atlasReq(t, ca, "GET", sources, "")
	if st != http.StatusOK || !strings.Contains(lb, "github.com/example/repo.git") {
		t.Fatalf("owner list: status=%d body=%s", st, lb)
	}

	// PATCH a source field (owner).
	patchURL := fmt.Sprintf("%s/api/atlas/sources/%d", ts.URL, srcID)
	if st, rb := atlasReq(t, ca, "PATCH", patchURL, `{"branch":"main"}`); st != http.StatusOK || !strings.Contains(rb, `"branch":"main"`) {
		t.Fatalf("owner patch source: status=%d body=%s", st, rb)
	}

	// Trigger a run: gating passes for the owner, then the enablement guard fires
	// (no embedder / LLM configured in tests) — a clean 503, not a crash.
	runURL := fmt.Sprintf("%s/api/atlas/sources/%d/run", ts.URL, srcID)
	if st, rb := atlasReq(t, ca, "POST", runURL, ""); st != http.StatusServiceUnavailable || !strings.Contains(rb, "ai_unavailable") {
		t.Fatalf("owner run: want 503 ai_unavailable, got %d %s", st, rb)
	}
	if st, _ := atlasReq(t, cb, "POST", runURL, ""); st != http.StatusForbidden {
		t.Fatalf("stranger run: want 403, got %d", st)
	}

	// Unknown run → 404.
	if st, _ := atlasReq(t, ca, "GET", fmt.Sprintf("%s/api/atlas/runs/999999", ts.URL), ""); st != http.StatusNotFound {
		t.Fatalf("missing run: want 404, got %d", st)
	}

	// Stranger can't delete; owner can.
	if st, _ := atlasReq(t, cb, "DELETE", patchURL, ""); st != http.StatusForbidden {
		t.Fatalf("stranger delete: want 403, got %d", st)
	}
	if st, _ := atlasReq(t, ca, "DELETE", patchURL, ""); st != http.StatusNoContent {
		t.Fatalf("owner delete: want 204, got %d", st)
	}
}

// TestAtlasJiraSourceValidation checks the jira source create gate: jira requires
// both a project key (subpath) and a credential; a git source needs neither.
func TestAtlasJiraSourceValidation(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	pid := seedAtlasProject(t, d, "Tracker Docs", accountUser, alice, 0, 0)
	ca := loginClient(t, ts, "alice", "alicepw12")
	sources := fmt.Sprintf("%s/api/atlas/projects/%d/sources", ts.URL, pid)

	// jira without subpath → 400.
	if st, rb := atlasReq(t, ca, "POST", sources, `{"type":"jira","location":"https://x.atlassian.net"}`); st != http.StatusBadRequest || !strings.Contains(rb, "subpath") {
		t.Fatalf("jira no subpath: want 400, got %d %s", st, rb)
	}
	// jira with subpath but no credential → 400.
	if st, rb := atlasReq(t, ca, "POST", sources, `{"type":"jira","location":"https://x.atlassian.net","subpath":"ATL"}`); st != http.StatusBadRequest || !strings.Contains(rb, "credential") {
		t.Fatalf("jira no credential: want 400, got %d %s", st, rb)
	}
	// jira with a real credential → created.
	cid := seedAtlasCredential(t, d, accountUser, alice, "jira1", "jira", "tok", map[string]string{"email": "me@x.com"})
	body := fmt.Sprintf(`{"type":"jira","location":"https://x.atlassian.net","subpath":"ATL","cred_id":%d}`, cid)
	if st, rb := atlasReq(t, ca, "POST", sources, body); st != http.StatusCreated {
		t.Fatalf("jira valid create: status=%d body=%s", st, rb)
	}
}

// TestAtlasCredReuse checks that one owner-scoped credential can be bound to many
// sources across the owner's projects, but a cred owned by a different owner is
// rejected.
func TestAtlasCredReuse(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	ca := loginClient(t, ts, "alice", "alicepw12")

	// This test is about credential reuse, not the source cap — lift the
	// per-tier Atlas source limit so two sources can be created.
	if _, err := d.ExecContext(context.Background(),
		`UPDATE plans SET max_atlas_sources = NULL WHERE key = 'personal_free'`); err != nil {
		t.Fatalf("lift source cap: %v", err)
	}

	// One credential, reused across two of Alice's projects.
	cred := seedAtlasCredential(t, d, accountUser, alice, "gh", "git", "ghp_tok", map[string]string{"username": "x-access-token"})
	p1 := seedAtlasProject(t, d, "Proj One", accountUser, alice, 0, 0)
	p2 := seedAtlasProject(t, d, "Proj Two", accountUser, alice, 0, 0)
	for _, pid := range []int64{p1, p2} {
		url := fmt.Sprintf("%s/api/atlas/projects/%d/sources", ts.URL, pid)
		body := fmt.Sprintf(`{"type":"git","location":"https://github.com/example/repo.git","name":"r","cred_id":%d}`, cred)
		if st, rb := atlasReq(t, ca, "POST", url, body); st != http.StatusCreated {
			t.Fatalf("reuse cred on project %d: status=%d body=%s", pid, st, rb)
		}
	}

	// A credential owned by Bob can't be bound to Alice's project.
	bobCred := seedAtlasCredential(t, d, accountUser, bob, "bobgh", "git", "ghp_bob", nil)
	url := fmt.Sprintf("%s/api/atlas/projects/%d/sources", ts.URL, p1)
	crossBody := fmt.Sprintf(`{"type":"git","location":"https://github.com/example/repo.git","cred_id":%d}`, bobCred)
	if st, rb := atlasReq(t, ca, "POST", url, crossBody); st != http.StatusBadRequest || !strings.Contains(rb, "invalid_credential") {
		t.Fatalf("cross-owner cred bind: want 400 invalid_credential, got %d %s", st, rb)
	}
}

// TestAtlasSourceQuota enforces the per-tier Atlas source cap
// (plans.max_atlas_sources): the default personal_free plan caps sources at 1,
// counted across ALL of the account's projects; raising the cap lifts the gate.
func TestAtlasSourceQuota(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	ca := loginClient(t, ts, "alice", "alicepw12")
	space := seedSpace(t, d, "Repo Docs", "repo-docs", alice)
	pa := seedAtlasProject(t, d, "Proj A", accountUser, alice, space, 0)
	pb := seedAtlasProject(t, d, "Proj B", accountUser, alice, space, 0)

	srcURL := func(pid int64) string {
		return fmt.Sprintf("%s/api/atlas/projects/%d/sources", ts.URL, pid)
	}
	src := func(name string) string {
		return fmt.Sprintf(`{"type":"git","location":"https://github.com/example/%s.git","name":"%s"}`, name, name)
	}

	// personal_free caps Atlas sources at 1 → the first connects.
	if st, rb := atlasReq(t, ca, "POST", srcURL(pa), src("one")); st != http.StatusCreated {
		t.Fatalf("first source: want 201, got %d %s", st, rb)
	}
	// A second source — even on a DIFFERENT project of the same account — is gated.
	if st, rb := atlasReq(t, ca, "POST", srcURL(pb), src("two")); st != http.StatusPaymentRequired || !strings.Contains(rb, "quota_exceeded") {
		t.Fatalf("second source (cross-project): want 402 quota_exceeded, got %d %s", st, rb)
	}
	// Raise the cap → the gate lifts.
	if _, err := d.ExecContext(context.Background(),
		`UPDATE plans SET max_atlas_sources = 5 WHERE key = 'personal_free'`); err != nil {
		t.Fatalf("raise cap: %v", err)
	}
	if st, rb := atlasReq(t, ca, "POST", srcURL(pb), src("two")); st != http.StatusCreated {
		t.Fatalf("after raising cap: want 201, got %d %s", st, rb)
	}
}
