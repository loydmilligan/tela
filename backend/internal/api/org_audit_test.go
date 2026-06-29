package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestOrgAudit_OrgAdminScopedView(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	ctx := context.Background()

	admin := seedUser(t, d, "orgadmin", "adminpw123", false)
	member := seedUser(t, d, "plainmember", "memberpw12", false)
	stranger := seedUser(t, d, "stranger", "strangerpw", false)

	var orgID, otherOrg int64
	if err := d.QueryRowContext(ctx, `INSERT INTO orgs (name, slug) VALUES ('Acme', 'acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := d.QueryRowContext(ctx, `INSERT INTO orgs (name, slug) VALUES ('Other', 'other') RETURNING id`).Scan(&otherOrg); err != nil {
		t.Fatalf("insert other org: %v", err)
	}
	// The org audit log is an Enterprise feature (migration 0059) — entitle Acme
	// so the view is reachable. The access checks below are independent of it.
	if _, err := d.ExecContext(ctx, `UPDATE orgs SET plan_key='org_enterprise' WHERE id=$1`, orgID); err != nil {
		t.Fatalf("entitle org: %v", err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO org_members (org_id, user_id, org_role) VALUES ($1, $2, 'admin')`, orgID, admin); err != nil {
		t.Fatalf("insert org admin: %v", err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO org_members (org_id, user_id, org_role) VALUES ($1, $2, 'member')`, orgID, member); err != nil {
		t.Fatalf("insert org member: %v", err)
	}

	// Two audit rows for our org, one for another org (must not leak).
	aid := admin
	writeAudit(ctx, d, &aid, "org_member.add", "org", orgID, "member: bob")
	writeAudit(ctx, d, &aid, "domain.map", "org", orgID, "acme.com → Acme")
	writeAudit(ctx, d, &aid, "org_member.add", "org", otherOrg, "member: eve")

	// Org admin sees only their org's rows.
	rec := routedRecorder("GET /api/orgs/{id}/audit", srv.ListOrgAudit,
		userRequest(http.MethodGet, "/api/orgs/"+intStr(orgID)+"/audit", "", authUser(admin, "orgadmin", false)))
	if rec.Code != http.StatusOK {
		t.Fatalf("org audit: code=%d body=%q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "org_member.add") || !strings.Contains(body, "domain.map") {
		t.Fatalf("missing org rows: body=%q", body)
	}
	if strings.Contains(body, "eve") {
		t.Fatalf("leaked another org's audit rows: body=%q", body)
	}

	// A plain org member (not admin) is forbidden.
	rec = routedRecorder("GET /api/orgs/{id}/audit", srv.ListOrgAudit,
		userRequest(http.MethodGet, "/api/orgs/"+intStr(orgID)+"/audit", "", authUser(member, "plainmember", false)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member org audit: code=%d body=%q want 403", rec.Code, rec.Body.String())
	}

	// A non-member is forbidden.
	rec = routedRecorder("GET /api/orgs/{id}/audit", srv.ListOrgAudit,
		userRequest(http.MethodGet, "/api/orgs/"+intStr(orgID)+"/audit", "", authUser(stranger, "stranger", false)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stranger org audit: code=%d body=%q want 403", rec.Code, rec.Body.String())
	}

	// An instance-admin passes via the virtual-admin path even without a row.
	super := seedUser(t, d, "super", "superpw1234", true)
	rec = routedRecorder("GET /api/orgs/{id}/audit", srv.ListOrgAudit,
		userRequest(http.MethodGet, "/api/orgs/"+intStr(orgID)+"/audit", "", authUser(super, "super", true)))
	if rec.Code != http.StatusOK {
		t.Fatalf("instance-admin org audit: code=%d body=%q want 200", rec.Code, rec.Body.String())
	}
}

// TestOrgAudit_RequiresEntitlement — the audit view is gated on the Enterprise
// "audit" entitlement: an org admin of a non-Enterprise org gets 402.
func TestOrgAudit_RequiresEntitlement(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	ctx := context.Background()

	admin := seedUser(t, d, "freeadmin", "adminpw123", false)
	var orgID int64
	if err := d.QueryRowContext(ctx, `INSERT INTO orgs (name, slug) VALUES ('Free Co', 'freeco') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO org_members (org_id, user_id, org_role) VALUES ($1, $2, 'admin')`, orgID, admin); err != nil {
		t.Fatalf("insert org admin: %v", err)
	}
	// org_free (default) — no audit entitlement.
	rec := routedRecorder("GET /api/orgs/{id}/audit", srv.ListOrgAudit,
		userRequest(http.MethodGet, "/api/orgs/"+intStr(orgID)+"/audit", "", authUser(admin, "freeadmin", false)))
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("unentitled org audit: code=%d body=%q want 402", rec.Code, rec.Body.String())
	}
}
