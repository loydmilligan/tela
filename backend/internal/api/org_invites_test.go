package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
)

func TestOrgInvite_CreateListRevoke(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	org := seedOrg(t, d, "Acme", "acme")
	adminID := seedUser(t, d, "alice", "alicepw12", false)
	seedOrgMember(t, d, org, adminID, orgRoleAdmin)
	admin := authUser(adminID, "alice", false)

	rec := routedRecorder("POST /api/orgs/{id}/invites", srv.CreateOrgInvite,
		userRequest(http.MethodPost, "/api/orgs/"+intStr(org)+"/invites", `{"email":"bob@acme.com","org_role":"member"}`, admin))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create invite = %d body=%q", rec.Code, rec.Body)
	}
	// A pending row exists.
	var n int
	mustQueryRow(t, d, `SELECT COUNT(*) FROM org_invites WHERE org_id=$1 AND lower(email)='bob@acme.com' AND accepted_at IS NULL`, &n, org)
	if n != 1 {
		t.Fatalf("expected 1 pending invite, got %d", n)
	}

	// Admin lists it.
	rec = routedRecorder("GET /api/orgs/{id}/invites", srv.ListOrgInvites,
		userRequest(http.MethodGet, "/api/orgs/"+intStr(org)+"/invites", "", admin))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "bob@acme.com") {
		t.Fatalf("list invites = %d body=%q", rec.Code, rec.Body)
	}

	// A non-admin can't manage invites.
	stranger := authUser(seedUser(t, d, "eve", "evepw1234", false), "eve", false)
	rec = routedRecorder("GET /api/orgs/{id}/invites", srv.ListOrgInvites,
		userRequest(http.MethodGet, "/api/orgs/"+intStr(org)+"/invites", "", stranger))
	if rec.Code == http.StatusOK {
		t.Fatalf("non-admin list invites should be forbidden, got %d", rec.Code)
	}

	// Get the id and revoke it.
	var id int64
	mustQueryRow(t, d, `SELECT id FROM org_invites WHERE org_id=$1 AND accepted_at IS NULL`, &id, org)
	rec = routedRecorder("DELETE /api/orgs/{id}/invites/{inviteId}", srv.RevokeOrgInvite,
		userRequest(http.MethodDelete, "/api/orgs/"+intStr(org)+"/invites/"+intStr(id), "", admin))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke invite = %d", rec.Code)
	}
}

func TestOrgInvite_GetPublicAndAccept(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	org := seedOrg(t, d, "Acme", "acme")
	raw := "raw-invite-token-1234567890"
	mustExec(t, d, `INSERT INTO org_invites (org_id, email, org_role, token_hash, expires_at)
		VALUES ($1, 'bob@acme.com', 'member', $2, to_char((now() AT TIME ZONE 'UTC') + interval '7 days', 'YYYY-MM-DD HH24:MI:SS'))`,
		org, hashEmailToken(raw))

	// Public lookup renders the accept page data without a session.
	rec := routedRecorder("GET /api/invites/{token}", srv.GetInvite,
		userRequest(http.MethodGet, "/api/invites/"+raw, "", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"valid":true`) ||
		!strings.Contains(rec.Body.String(), "Acme") || !strings.Contains(rec.Body.String(), "bob@acme.com") {
		t.Fatalf("get invite = %d body=%q", rec.Code, rec.Body)
	}
	// A bogus token is simply invalid (no leak).
	rec = routedRecorder("GET /api/invites/{token}", srv.GetInvite,
		userRequest(http.MethodGet, "/api/invites/nope", "", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"valid":false`) {
		t.Fatalf("bogus invite = %d body=%q", rec.Code, rec.Body)
	}

	// Wrong-email user is refused (invites are email-targeted).
	eveID := seedUser(t, d, "eve", "evepw1234", false)
	eve := &auth.User{ID: eveID, Username: "eve", Email: "eve@other.com"}
	rec = recordHandler(srv.AcceptInvite, userRequest(http.MethodPost, "/api/me/accept-invite", `{"token":"`+raw+`"}`, eve))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong-email accept = %d; want 403", rec.Code)
	}

	// The invited user accepts and joins.
	bobID := seedUser(t, d, "bob", "bobpw1234", false)
	bob := &auth.User{ID: bobID, Username: "bob", Email: "bob@acme.com"}
	rec = recordHandler(srv.AcceptInvite, userRequest(http.MethodPost, "/api/me/accept-invite", `{"token":"`+raw+`"}`, bob))
	if rec.Code != http.StatusOK {
		t.Fatalf("accept = %d body=%q", rec.Code, rec.Body)
	}
	var n int
	mustQueryRow(t, d, `SELECT COUNT(*) FROM org_members WHERE org_id=$1 AND user_id=$2 AND org_role='member'`, &n, org, bobID)
	if n != 1 {
		t.Fatalf("bob should be a member, got %d", n)
	}
	// The invite is now consumed — re-accepting fails.
	rec = recordHandler(srv.AcceptInvite, userRequest(http.MethodPost, "/api/me/accept-invite", `{"token":"`+raw+`"}`, bob))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("re-accept consumed invite = %d; want 400", rec.Code)
	}
}

func TestOrgInvite_AutoJoinOnVerify(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	ctx := context.Background()
	org := seedOrg(t, d, "Acme", "acme")
	mustExec(t, d, `INSERT INTO org_invites (org_id, email, org_role, token_hash, expires_at)
		VALUES ($1, 'carol@acme.com', 'admin', 'somehash', to_char((now() AT TIME ZONE 'UTC') + interval '7 days', 'YYYY-MM-DD HH24:MI:SS'))`, org)

	carolID := seedUser(t, d, "carol", "carolpw12", false)
	// Mirrors the VerifyEmail hook: a freshly-verified address auto-joins its invites.
	srv.applyPendingInvites(ctx, carolID, "carol@acme.com")

	var role string
	if err := d.QueryRowContext(ctx, `SELECT org_role FROM org_members WHERE org_id=$1 AND user_id=$2`, org, carolID).Scan(&role); err != nil || role != "admin" {
		t.Fatalf("carol should have auto-joined as admin, role=%q err=%v", role, err)
	}
	var accepted int
	mustQueryRow(t, d, `SELECT COUNT(*) FROM org_invites WHERE org_id=$1 AND accepted_at IS NOT NULL`, &accepted, org)
	if accepted != 1 {
		t.Fatalf("invite should be marked accepted, got %d", accepted)
	}
}
