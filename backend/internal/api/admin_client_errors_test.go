package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

type clientErrorGroupsResp struct {
	Groups []clientErrorGroupDTO `json:"groups"`
}

func postClientError(t *testing.T, c *http.Client, base string, payload map[string]any) {
	t.Helper()
	b, _ := json.Marshal(payload)
	resp, err := c.Post(base+"/api/client-errors", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("post client error: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("post client error status=%d", resp.StatusCode)
	}
}

// TestClientErrorGroups_GroupsAndNormalizes — repeated identical errors collapse
// to one group with the right count, and messages differing only by ids/numbers
// fold together via fingerprint normalization.
func TestClientErrorGroups_GroupsAndNormalizes(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "admin", "testpass123", true)
	c := loginClient(t, ts, "admin", "testpass123")

	// Same error 3×.
	for i := 0; i < 3; i++ {
		postClientError(t, c, ts.URL, map[string]any{"kind": "react", "message": "boom", "stack": "at X (a.js:10)"})
	}
	// Differ only by a number in the message → must group into one.
	postClientError(t, c, ts.URL, map[string]any{"kind": "query", "message": "page 1 failed"})
	postClientError(t, c, ts.URL, map[string]any{"kind": "query", "message": "page 2 failed"})
	// A genuinely distinct one.
	postClientError(t, c, ts.URL, map[string]any{"kind": "error", "message": "totally different"})

	resp, err := c.Get(ts.URL + "/api/admin/client-errors")
	if err != nil {
		t.Fatalf("get groups: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var got clientErrorGroupsResp
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Groups) != 3 {
		t.Fatalf("want 3 groups, got %d: %+v", len(got.Groups), got.Groups)
	}

	byMsg := map[string]clientErrorGroupDTO{}
	for _, g := range got.Groups {
		byMsg[g.Message] = g
	}
	if g := byMsg["boom"]; g.Count != 3 || g.Kind != "react" || g.Users != 1 {
		t.Fatalf("boom group wrong: %+v", g)
	}
	// The two page-N errors share a fingerprint; the sample message is whichever
	// landed last, so assert on count via the fingerprint group instead of msg.
	var paged *clientErrorGroupDTO
	for i := range got.Groups {
		if got.Groups[i].Kind == "query" {
			paged = &got.Groups[i]
		}
	}
	if paged == nil || paged.Count != 2 {
		t.Fatalf("expected one query group with count 2, got %+v", paged)
	}

	// Drill-down: the boom fingerprint has 3 occurrences.
	fp := byMsg["boom"].Fingerprint
	resp2, err := c.Get(ts.URL + "/api/admin/client-errors/" + fp)
	if err != nil {
		t.Fatalf("get occurrences: %v", err)
	}
	defer resp2.Body.Close()
	var occ struct {
		Occurrences []clientErrorOccurrenceDTO `json:"occurrences"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&occ); err != nil {
		t.Fatalf("decode occ: %v", err)
	}
	if len(occ.Occurrences) != 3 {
		t.Fatalf("want 3 occurrences, got %d", len(occ.Occurrences))
	}
}

// TestClientErrorGroups_AdminOnly — a non-admin is forbidden.
func TestClientErrorGroups_AdminOnly(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "bob", "testpass123", false)
	c := loginClient(t, ts, "bob", "testpass123")

	resp, err := c.Get(ts.URL + "/api/admin/client-errors")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want 403", resp.StatusCode)
	}
}
