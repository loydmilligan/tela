package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The http_sd feed lists only ACTIVE custom domains, each as a /api/health probe
// target — so the monitoring stack picks up a new domain the moment it verifies
// and drops it when removed, with no per-domain config.
func TestBlackboxTargets(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "admin", "adminpass1", true)
	org := seedOrg(t, d, "Acme", "acme")
	c := loginClient(t, ts, "admin", "adminpass1")

	// One active, one pending — only the active one may surface.
	if _, err := d.Exec(
		`INSERT INTO org_hostnames (hostname, org_id, status, verify_token)
		 VALUES ($1,$2,'active',$3), ($4,$2,'pending',$5)`,
		"wiki.acme.example", org, "t1", "soon.acme.example", "t2"); err != nil {
		t.Fatalf("insert hostnames: %v", err)
	}

	resp, err := c.Get(ts.URL + "/api/admin/blackbox-targets")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var groups []sdTargetGroup
	if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1 (active only)", len(groups))
	}
	if got := groups[0].Targets[0]; got != "https://wiki.acme.example/api/health" {
		t.Errorf("target = %q, want https://wiki.acme.example/api/health", got)
	}
	if got := groups[0].Labels["custom_domain"]; got != "wiki.acme.example" {
		t.Errorf("label custom_domain = %q, want wiki.acme.example", got)
	}
}

// Non-admins can't enumerate custom domains through the feed.
func TestBlackboxTargetsRequiresAdmin(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "plainuser", "password1", false)
	c := loginClient(t, ts, "plainuser", "password1")

	resp, err := c.Get(ts.URL + "/api/admin/blackbox-targets")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("status = 200 for non-admin, want a rejection")
	}
}
