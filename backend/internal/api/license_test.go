package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/zcag/tela/backend/internal/ee"
)

// TestEntitledViaLicense — the self-host unlock path: a license that grants a
// feature entitles an account even on a plan whose flag doesn't, while the cloud
// plan-flag path still works without a license.
func TestEntitledViaLicense(t *testing.T) {
	d := newAPITestDB(t)
	s := &Server{DB: d}
	ctx := context.Background()
	org := seedOrg(t, d, "Acme", "acme") // org_free — no sso plan flag
	acct := account{accountOrg, org}

	// No license, free plan → not entitled.
	if s.entitled(ctx, acct, "sso") {
		t.Fatal("free org without a license must not be entitled to sso")
	}

	// A license granting sso entitles the org even on the free plan.
	s.license = &ee.License{Tier: "enterprise", Features: map[string]bool{"sso": true}}
	if !s.entitled(ctx, acct, "sso") {
		t.Fatal("license granting sso should entitle the org")
	}
	if s.entitled(ctx, acct, "scim") {
		t.Fatal("license without scim must not entitle scim")
	}

	// The cloud plan-flag path still works with no license installed.
	s.license = nil
	mustExec(t, d, `UPDATE orgs SET plan_key='org_enterprise' WHERE id=$1`, org)
	if !s.entitled(ctx, acct, "sso") {
		t.Fatal("enterprise plan should entitle sso via the plan flag")
	}
}

// TestLicenseAPI_AdminFlow — the admin endpoints are instance-admin gated, report
// no license cleanly, and reject a malformed key up front.
func TestLicenseAPI_AdminFlow(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw123", true)
	userID := seedUser(t, d, "bob", "bobpw12345", false)

	// Non-admin is rejected.
	if rec := recordHandler(srv.GetLicense, userRequest(http.MethodGet, "/api/admin/license", "", authUser(userID, "bob", false))); rec.Code == http.StatusOK {
		t.Fatalf("non-admin GET license should be rejected, got %d", rec.Code)
	}

	// Admin GET with no license installed → valid:false.
	rec := recordHandler(srv.GetLicense, userRequest(http.MethodGet, "/api/admin/license", "", authUser(adminID, "admin", true)))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET license: status=%d body=%q", rec.Code, rec.Body.String())
	}
	var got struct {
		License ee.Status `json:"license"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.License.Valid {
		t.Fatal("no license installed should report valid:false")
	}

	// A malformed key is rejected before persistence.
	put := recordHandler(srv.PutLicense, userRequest(http.MethodPut, "/api/admin/license", `{"token":"not-a-real-key"}`, authUser(adminID, "admin", true)))
	if put.Code != http.StatusBadRequest {
		t.Fatalf("malformed license PUT: want 400, got %d body=%q", put.Code, put.Body.String())
	}
}
