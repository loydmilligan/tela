package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/atlas/core"
)

// seedAtlasSecret inserts a secret into a space and returns its id.
func seedAtlasSecret(t *testing.T, d *sql.DB, spaceID int64, name, kind, value string, meta map[string]string) int64 {
	t.Helper()
	metaJSON := ""
	if len(meta) > 0 {
		b, _ := json.Marshal(meta)
		metaJSON = string(b)
	}
	var id int64
	if err := d.QueryRow(
		`INSERT INTO atlas_secrets (space_id, name, kind, value, meta_json) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		spaceID, name, kind, value, metaJSON).Scan(&id); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	return id
}

// TestAtlasSecrets_CRUDAndScoping locks the secret store's access model + the
// write-only contract: management (create/list/delete) is owner/org-admin only;
// the token value is never returned on read; and a source can't bind a secret
// from a different space.
func TestAtlasSecrets_CRUDAndScoping(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)      // owner of spaceA
	charlie := seedUser(t, d, "charlie", "charliepw1", false) // editor of spaceA
	bob := seedUser(t, d, "bob", "bobpw1234", false)          // owner of spaceB
	spaceA := seedSpace(t, d, "Repo Docs", "repo-docs", alice)
	spaceB := seedSpace(t, d, "Other", "other", bob)
	seedMember(t, d, spaceA, charlie, "editor")

	ca := loginClient(t, ts, "alice", "alicepw12")
	cc := loginClient(t, ts, "charlie", "charliepw1")
	cb := loginClient(t, ts, "bob", "bobpw1234")

	secretsA := fmt.Sprintf("%s/api/spaces/%d/atlas/secrets", ts.URL, spaceA)

	// Owner creates a git secret; the token is accepted but never echoed.
	body := `{"name":"gh","kind":"git","value":"ghp_supersecret","meta":{"username":"x-access-token"}}`
	st, resp := atlasReq(t, ca, "POST", secretsA, body)
	if st != http.StatusCreated {
		t.Fatalf("owner create secret: status=%d body=%s", st, resp)
	}
	if strings.Contains(resp, "ghp_supersecret") {
		t.Fatalf("create response leaked the token value: %s", resp)
	}
	var created struct {
		Secret struct {
			ID int64 `json:"id"`
		} `json:"secret"`
	}
	if json.Unmarshal([]byte(resp), &created) != nil || created.Secret.ID == 0 {
		t.Fatalf("decode created secret: %s", resp)
	}
	secretID := created.Secret.ID

	// Editor is denied management; non-member of spaceA (bob) too.
	if st, _ := atlasReq(t, cc, "POST", secretsA, body); st != http.StatusForbidden {
		t.Fatalf("editor create secret: want 403, got %d", st)
	}
	if st, _ := atlasReq(t, cb, "GET", secretsA, ""); st != http.StatusForbidden {
		t.Fatalf("non-member list secrets: want 403, got %d", st)
	}

	// List (owner): the value is blanked everywhere.
	st, lb := atlasReq(t, ca, "GET", secretsA, "")
	if st != http.StatusOK || strings.Contains(lb, "ghp_supersecret") {
		t.Fatalf("owner list secrets: status=%d body=%s", st, lb)
	}
	if !strings.Contains(lb, `"name":"gh"`) || !strings.Contains(lb, `"username":"x-access-token"`) {
		t.Fatalf("owner list secrets missing non-secret fields: %s", lb)
	}

	// jira secret requires an email in meta.
	if st, _ := atlasReq(t, ca, "POST", secretsA, `{"name":"jira1","kind":"jira","value":"tok"}`); st != http.StatusBadRequest {
		t.Fatalf("jira secret without email: want 400, got %d", st)
	}

	// A source in spaceA may bind its own secret…
	sourcesA := fmt.Sprintf("%s/api/spaces/%d/atlas/sources", ts.URL, spaceA)
	okBody := fmt.Sprintf(`{"type":"git","location":"https://github.com/example/repo.git","name":"r","secret_id":%d}`, secretID)
	if st, rb := atlasReq(t, ca, "POST", sourcesA, okBody); st != http.StatusCreated {
		t.Fatalf("bind own-space secret: status=%d body=%s", st, rb)
	}

	// …but a source in spaceB may NOT bind spaceA's secret (cross-space rejected).
	sourcesB := fmt.Sprintf("%s/api/spaces/%d/atlas/sources", ts.URL, spaceB)
	crossBody := fmt.Sprintf(`{"type":"git","location":"https://github.com/example/repo.git","secret_id":%d}`, secretID)
	if st, rb := atlasReq(t, cb, "POST", sourcesB, crossBody); st != http.StatusBadRequest || !strings.Contains(rb, "invalid_secret") {
		t.Fatalf("cross-space secret bind: want 400 invalid_secret, got %d %s", st, rb)
	}

	// Delete is management-gated: editor denied, owner allowed.
	delURL := fmt.Sprintf("%s/api/atlas/secrets/%d", ts.URL, secretID)
	if st, _ := atlasReq(t, cc, "DELETE", delURL, ""); st != http.StatusForbidden {
		t.Fatalf("editor delete secret: want 403, got %d", st)
	}
	if st, _ := atlasReq(t, ca, "DELETE", delURL, ""); st != http.StatusNoContent {
		t.Fatalf("owner delete secret: want 204, got %d", st)
	}
}

// TestAtlasJiraSourceValidation checks the jira source create gate: jira requires
// both a project key (subpath) and a secret; a git source still needs neither.
func TestAtlasJiraSourceValidation(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	space := seedSpace(t, d, "Tracker Docs", "tracker-docs", alice)
	ca := loginClient(t, ts, "alice", "alicepw12")
	sources := fmt.Sprintf("%s/api/spaces/%d/atlas/sources", ts.URL, space)

	// jira without subpath → 400.
	if st, rb := atlasReq(t, ca, "POST", sources, `{"type":"jira","location":"https://x.atlassian.net"}`); st != http.StatusBadRequest || !strings.Contains(rb, "subpath") {
		t.Fatalf("jira no subpath: want 400, got %d %s", st, rb)
	}
	// jira with subpath but no secret → 400.
	if st, rb := atlasReq(t, ca, "POST", sources, `{"type":"jira","location":"https://x.atlassian.net","subpath":"ATL"}`); st != http.StatusBadRequest || !strings.Contains(rb, "secret") {
		t.Fatalf("jira no secret: want 400, got %d %s", st, rb)
	}
	// jira with a real secret → created.
	sid := seedAtlasSecret(t, d, space, "jira1", "jira", "tok", map[string]string{"email": "me@x.com"})
	body := fmt.Sprintf(`{"type":"jira","location":"https://x.atlassian.net","subpath":"ATL","secret_id":%d}`, sid)
	if st, rb := atlasReq(t, ca, "POST", sources, body); st != http.StatusCreated {
		t.Fatalf("jira valid create: status=%d body=%s", st, rb)
	}
}

// TestAtlasSecretResolution verifies the executor pre-resolves a source's secret_id
// into the run's transient core.Source: a git source's token is injected into the
// clone Location (the git connector authenticates via the URL), while a jira
// source gets SecretValue + SecretMeta["email"] with its Location (base) intact.
func TestAtlasSecretResolution(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "alice", "alicepw12", false)
	space := seedSpace(t, d, "Repo Docs", "repo-docs", owner)
	ctx := context.Background()

	// git: token injected into the clone URL userinfo.
	gitSecret := seedAtlasSecret(t, d, space, "gh", "git", "ghp_tok", map[string]string{"username": "x-access-token"})
	var gitSrcID int64
	if err := d.QueryRow(
		`INSERT INTO atlas_sources (space_id, type, location, name, secret_id, auto_update)
		 VALUES ($1,'git','https://github.com/example/repo.git','r',$2,0) RETURNING id`,
		space, gitSecret).Scan(&gitSrcID); err != nil {
		t.Fatalf("seed git source: %v", err)
	}
	gitRow, err := srv.atlas.loadSource(ctx, gitSrcID)
	if err != nil {
		t.Fatalf("load git source: %v", err)
	}
	if gitRow.SecretID == nil || *gitRow.SecretID != gitSecret {
		t.Fatalf("git source secret_id not loaded: %+v", gitRow.SecretID)
	}
	gitRC := srv.atlas.buildRunContext(ctx, gitRow, &core.Run{ID: 1}, t.TempDir())
	if gitRC.Source.SecretValue != "ghp_tok" {
		t.Fatalf("git SecretValue: got %q", gitRC.Source.SecretValue)
	}
	if want := "https://x-access-token:ghp_tok@github.com/example/repo.git"; gitRC.Source.Location != want {
		t.Fatalf("git Location not authed: got %q want %q", gitRC.Source.Location, want)
	}

	// jira: token + email on the transient source, base URL (Location) untouched.
	jiraSecret := seedAtlasSecret(t, d, space, "jira", "jira", "jtok", map[string]string{"email": "me@x.com"})
	var jiraSrcID int64
	if err := d.QueryRow(
		`INSERT INTO atlas_sources (space_id, type, location, name, subpath, secret_id, auto_update)
		 VALUES ($1,'jira','https://x.atlassian.net','t','ATL',$2,0) RETURNING id`,
		space, jiraSecret).Scan(&jiraSrcID); err != nil {
		t.Fatalf("seed jira source: %v", err)
	}
	jiraRow, err := srv.atlas.loadSource(ctx, jiraSrcID)
	if err != nil {
		t.Fatalf("load jira source: %v", err)
	}
	jiraRC := srv.atlas.buildRunContext(ctx, jiraRow, &core.Run{ID: 2}, t.TempDir())
	if jiraRC.Source.SecretValue != "jtok" || jiraRC.Source.SecretMeta["email"] != "me@x.com" {
		t.Fatalf("jira creds: value=%q meta=%v", jiraRC.Source.SecretValue, jiraRC.Source.SecretMeta)
	}
	if jiraRC.Source.Location != "https://x.atlassian.net" {
		t.Fatalf("jira Location should be untouched, got %q", jiraRC.Source.Location)
	}
}
