package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
)

func seedOrg(t *testing.T, d *sql.DB, name, slug string) int64 {
	t.Helper()
	var id int64
	err := d.QueryRowContext(context.Background(),
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`, name, slug).Scan(&id)
	if err != nil {
		t.Fatalf("insert org: %v", err)
	}
	return id
}

func seedOrgMember(t *testing.T, d *sql.DB, orgID, userID int64, role string) {
	t.Helper()
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO org_members (org_id, user_id, org_role) VALUES ($1, $2, $3)`,
		orgID, userID, role); err != nil {
		t.Fatalf("insert org_member: %v", err)
	}
}

func seedSpaceGrant(t *testing.T, d *sql.DB, spaceID, orgID int64, role string) {
	t.Helper()
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO space_grants (space_id, principal_kind, principal_id, role) VALUES ($1, 'org', $2, $3)`,
		spaceID, orgID, role); err != nil {
		t.Fatalf("insert space_grant: %v", err)
	}
}

// The headline: a space shared with an org confers access to every org member
// through the space_access view, resolved by spaceRole.
func TestOrgGrant_ConfersSpaceAccess(t *testing.T) {
	d := newAPITestDB(t)
	owner := seedUser(t, d, "owner", "ownerpw12", false)
	member := seedUser(t, d, "member", "memberpw1", false)
	stranger := seedUser(t, d, "stranger", "strangerp", false)

	space := seedSpace(t, d, "Docs", "docs", owner)
	org := seedOrg(t, d, "Acme", "acme")
	seedOrgMember(t, d, org, member, orgRoleMember)
	seedSpaceGrant(t, d, space, org, roleEditor)

	ctx := context.Background()
	if role, err := spaceRole(ctx, d, member, space); err != nil || role != roleEditor {
		t.Fatalf("org member effective role = %q, %v; want editor", role, err)
	}
	if role, err := spaceRole(ctx, d, owner, space); err != nil || role != roleOwner {
		t.Fatalf("direct owner role = %q, %v; want owner", role, err)
	}
	if _, err := spaceRole(ctx, d, stranger, space); err != sql.ErrNoRows {
		t.Fatalf("stranger role err = %v; want ErrNoRows", err)
	}
}

// A direct grant and an org grant for the same user resolve to the highest role.
func TestOrgGrant_MaxRoleWins(t *testing.T) {
	d := newAPITestDB(t)
	owner := seedUser(t, d, "owner", "ownerpw12", false)
	u := seedUser(t, d, "u", "upassword", false)

	space := seedSpace(t, d, "Docs", "docs", owner)
	seedMember(t, d, space, u, roleViewer) // direct viewer
	org := seedOrg(t, d, "Acme", "acme")
	seedOrgMember(t, d, org, u, orgRoleMember)
	seedSpaceGrant(t, d, space, org, roleEditor) // org editor — should win

	if role, err := spaceRole(context.Background(), d, u, space); err != nil || role != roleEditor {
		t.Fatalf("effective role = %q, %v; want editor (max of viewer/editor)", role, err)
	}
}

// Deleting an org tears down its grants (no FK on the polymorphic principal).
func TestDeleteOrg_RemovesGrantsAndAccess(t *testing.T) {
	d := newAPITestDB(t)
	admin := authUser(seedUser(t, d, "admin", "adminpw12", true), "admin", true)
	owner := seedUser(t, d, "owner", "ownerpw12", false)
	member := seedUser(t, d, "member", "memberpw1", false)
	space := seedSpace(t, d, "Docs", "docs", owner)
	org := seedOrg(t, d, "Acme", "acme")
	seedOrgMember(t, d, org, member, orgRoleMember)
	seedSpaceGrant(t, d, space, org, roleEditor)

	srv := New(d)
	req := userRequest(http.MethodDelete, "/api/orgs/1", "", admin)
	rec := routedRecorder("DELETE /api/orgs/{id}", srv.DeleteOrg, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete org status = %d; want 204 (%s)", rec.Code, rec.Body)
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM space_grants WHERE principal_id = $1`, org).Scan(&n)
	if n != 0 {
		t.Fatalf("space_grants for deleted org = %d; want 0", n)
	}
	if _, err := spaceRole(context.Background(), d, member, space); err != sql.ErrNoRows {
		t.Fatalf("member still has access after org delete: %v", err)
	}
}

func TestCreateOrg_SelfServe(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d) // managedCloud=true under TELA_CLOUD=1 → self-serve enabled
	ctx := context.Background()
	bobID := seedUser(t, d, "bob", "bobpw1234", false)
	nonAdmin := authUser(bobID, "bob", false)

	// Self-serve: a regular user can create a team and becomes its admin.
	rec := recordHandler(srv.CreateOrg, userRequest(http.MethodPost, "/api/orgs", `{"name":"Bob Team"}`, nonAdmin))
	if rec.Code != http.StatusCreated {
		t.Fatalf("self-serve create org = %d; want 201 (%s)", rec.Code, rec.Body)
	}
	var created struct {
		Org struct {
			ID int64 `json:"id"`
		} `json:"org"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	var role string
	if err := d.QueryRow(`SELECT org_role FROM org_members WHERE org_id=$1 AND user_id=$2`, created.Org.ID, bobID).Scan(&role); err != nil || role != "admin" {
		t.Fatalf("creator should be org admin, got role=%q err=%v", role, err)
	}

	// With self-serve disabled, a non-admin is refused.
	if err := srv.settings.Set(ctx, "allow_org_self_serve", "0", nil); err != nil {
		t.Fatal(err)
	}
	rec = recordHandler(srv.CreateOrg, userRequest(http.MethodPost, "/api/orgs", `{"name":"Nope"}`, nonAdmin))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("self-serve disabled non-admin = %d; want 403", rec.Code)
	}

	// An instance admin is exempt from the flag and derives the slug.
	admin := authUser(seedUser(t, d, "admin", "adminpw12", true), "admin", true)
	rec = recordHandler(srv.CreateOrg, userRequest(http.MethodPost, "/api/orgs", `{"name":"Acme Inc"}`, admin))
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create org = %d; want 201 (%s)", rec.Code, rec.Body)
	}
	var out struct {
		Org struct {
			Slug string `json:"slug"`
		} `json:"org"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Org.Slug != "acme-inc" {
		t.Fatalf("derived slug = %q; want acme-inc", out.Org.Slug)
	}
}

func TestOrgMembers_LastAdminSafeguard(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw12", false)
	org := seedOrg(t, d, "Acme", "acme")
	seedOrgMember(t, d, org, adminID, orgRoleAdmin)
	admin := authUser(adminID, "admin", false)

	// Demoting the sole admin is refused.
	req := userRequest(http.MethodPatch, "/api/orgs/1/members/1", `{"org_role":"member"}`, admin)
	rec := routedRecorder("PATCH /api/orgs/{id}/members/{user_id}", srv.PatchOrgMember, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("demote last admin = %d; want 400 (%s)", rec.Code, rec.Body)
	}
}

// Org grants are owner-gated and may only be editor/viewer.
func TestAddSpaceGrant_OwnerGatedAndRoleRestricted(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	ownerID := seedUser(t, d, "owner", "ownerpw12", false)
	editorID := seedUser(t, d, "editor", "editorpw1", false)
	space := seedSpace(t, d, "Docs", "docs", ownerID)
	seedMember(t, d, space, editorID, roleEditor)
	org := seedOrg(t, d, "Acme", "acme")

	_ = org
	// Editor (non-owner) is refused.
	req := userRequest(http.MethodPost, "/api/spaces/1/grants", `{"principal_kind":"org","principal_id":1,"role":"viewer"}`, authUser(editorID, "editor", false))
	rec := routedRecorder("POST /api/spaces/{id}/grants", srv.AddSpaceGrant, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor add grant = %d; want 403", rec.Code)
	}

	// Owner granting 'owner' to an org is rejected (reserved for direct users).
	req = userRequest(http.MethodPost, "/api/spaces/1/grants", `{"principal_kind":"org","principal_id":1,"role":"owner"}`, authUser(ownerID, "owner", false))
	rec = routedRecorder("POST /api/spaces/{id}/grants", srv.AddSpaceGrant, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("owner grant role=owner = %d; want 400 (%s)", rec.Code, rec.Body)
	}

	// Owner granting editor succeeds.
	req = userRequest(http.MethodPost, "/api/spaces/1/grants", `{"principal_kind":"org","principal_id":1,"role":"editor"}`, authUser(ownerID, "owner", false))
	rec = routedRecorder("POST /api/spaces/{id}/grants", srv.AddSpaceGrant, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("owner add editor grant = %d; want 201 (%s)", rec.Code, rec.Body)
	}
}

// End-to-end through the real router + middleware: a member of an org that's
// been granted a space sees that space in GET /api/spaces and can open it.
func TestIntegration_OrgGrantGivesSpaceAccess(t *testing.T) {
	ts, d := newWiredServer(t)
	owner := seedUser(t, d, "owner", "ownerpw12", false)
	seedUser(t, d, "member", "memberpw1", false)
	memberID := int64(2)

	space := seedSpace(t, d, "Team Docs", "team-docs", owner)
	org := seedOrg(t, d, "Acme", "acme")
	seedOrgMember(t, d, org, memberID, orgRoleMember)
	seedSpaceGrant(t, d, space, org, roleEditor)

	c := loginClient(t, ts, "member", "memberpw1")
	resp, err := c.Get(ts.URL + "/api/spaces")
	if err != nil {
		t.Fatalf("list spaces: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("list spaces status=%d body=%s", resp.StatusCode, b)
	}
	var out struct {
		Spaces []struct {
			ID   int64  `json:"id"`
			Slug string `json:"slug"`
		} `json:"spaces"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Spaces) != 1 || out.Spaces[0].Slug != "team-docs" {
		t.Fatalf("member spaces = %+v; want the org-granted space", out.Spaces)
	}

	// And the space detail route resolves (membership gate passes via the view).
	r2, err := c.Get(ts.URL + "/api/spaces/" + strconv.FormatInt(space, 10))
	if err != nil {
		t.Fatalf("get space: %v", err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("get granted space status=%d; want 200", r2.StatusCode)
	}
}

// Invariant 1: org/group grants can never be 'owner' — enforced at the DB layer
// (trigger), independent of the API check.
func TestSpaceGrant_OwnerPrincipalRejectedByDB(t *testing.T) {
	d := newAPITestDB(t)
	owner := seedUser(t, d, "owner", "ownerpw12", false)
	space := seedSpace(t, d, "Docs", "docs", owner)
	org := seedOrg(t, d, "Acme", "acme")

	_, err := d.Exec(
		`INSERT INTO space_grants (space_id, principal_kind, principal_id, role) VALUES ($1, 'org', $2, 'owner')`,
		space, org)
	if err == nil {
		t.Fatal("inserting an org grant with role=owner should be rejected by the trigger")
	}
	// And the same via UPDATE from a legit editor grant.
	seedSpaceGrant(t, d, space, org, roleEditor)
	if _, err := d.Exec(
		`UPDATE space_grants SET role='owner' WHERE space_id=$1 AND principal_id=$2`, space, org); err == nil {
		t.Fatal("updating an org grant to role=owner should be rejected by the trigger")
	}
}

// Identity-derived membership can't be removed while the domain mapping stands.
func TestDeleteOrgMember_DomainManagedBlocked(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw12", false)
	memberID := seedUser(t, d, "carol", "carolpw12", false)
	if _, err := d.Exec(`UPDATE users SET email='carol@acme.com' WHERE id=$1`, memberID); err != nil {
		t.Fatalf("set email: %v", err)
	}
	org := seedOrg(t, d, "Acme", "acme")
	seedOrgMember(t, d, org, adminID, orgRoleAdmin)
	seedOrgMember(t, d, org, memberID, orgRoleMember)
	if _, err := d.Exec(`INSERT INTO org_email_domains (domain, org_id) VALUES ('acme.com', $1)`, org); err != nil {
		t.Fatalf("seed domain: %v", err)
	}

	if !isDomainManagedMember(context.Background(), d, org, memberID) {
		t.Fatalf("precondition: member should be domain-managed (org=%d member=%d)", org, memberID)
	}

	req := userRequest(http.MethodDelete, "/api/orgs/1/members/2", "", authUser(adminID, "admin", false))
	rec := routedRecorder("DELETE /api/orgs/{id}/members/{user_id}", srv.DeleteOrgMember, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("remove domain-managed member = %d; want 409 (%s)", rec.Code, rec.Body)
	}
}

// The effective-access endpoint resolves direct + org sources and the max role.
func TestGetSpaceAccess_ResolvesSourcesAndEffectiveRole(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw12", false)
	dual := seedUser(t, d, "dual", "dualpw123", false) // direct viewer + org editor
	space := seedSpace(t, d, "Docs", "docs", owner)
	seedMember(t, d, space, dual, roleViewer)
	org := seedOrg(t, d, "Acme", "acme")
	seedOrgMember(t, d, org, dual, orgRoleMember)
	seedSpaceGrant(t, d, space, org, roleEditor)

	req := userRequest(http.MethodGet, "/api/spaces/1/access", "", authUser(owner, "owner", false))
	rec := routedRecorder("GET /api/spaces/{id}/access", srv.GetSpaceAccess, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get access = %d; want 200 (%s)", rec.Code, rec.Body)
	}
	var out struct {
		Access []struct {
			UserID        int64  `json:"user_id"`
			EffectiveRole string `json:"effective_role"`
			Sources       []struct {
				Kind string `json:"kind"`
			} `json:"sources"`
		} `json:"access"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Access) != 2 {
		t.Fatalf("access entries = %d; want 2", len(out.Access))
	}
	var found bool
	for _, a := range out.Access {
		if a.UserID == dual {
			found = true
			if a.EffectiveRole != roleEditor {
				t.Fatalf("dual effective role = %q; want editor (max of viewer/editor)", a.EffectiveRole)
			}
			if len(a.Sources) != 2 {
				t.Fatalf("dual sources = %d; want 2 (direct + org)", len(a.Sources))
			}
		}
	}
	if !found {
		t.Fatal("dual user missing from access list")
	}
}

// An instance-admin who is ALSO a plain member of an org keeps full admin
// authority over it (the membership row must not downgrade their superuser
// power). Regression for the requireOrgMember instance-admin-first fix.
func TestInstanceAdmin_RetainsOrgAuthorityWhenMember(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw12", true) // instance-admin
	org := seedOrg(t, d, "Acme", "acme")
	seedOrgMember(t, d, org, adminID, orgRoleMember) // joined as a plain member

	// Admin (instance-admin) creates a group despite holding only 'member' in
	// the org — must succeed, not 403.
	req := userRequest(http.MethodPost, "/api/orgs/1/groups", `{"name":"Eng"}`, authUser(adminID, "admin", true))
	rec := routedRecorder("POST /api/orgs/{id}/groups", srv.CreateGroup, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("instance-admin (org member) create group = %d; want 201 (%s)", rec.Code, rec.Body)
	}
}

func seedGroup(t *testing.T, d *sql.DB, orgID int64, name string) int64 {
	t.Helper()
	var id int64
	err := d.QueryRowContext(context.Background(),
		`INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id`, orgID, name).Scan(&id)
	if err != nil {
		t.Fatalf("insert group: %v", err)
	}
	return id
}

// A space shared with a group confers access to its members via the rebuilt
// space_access view — the third principal leg.
func TestGroupGrant_ConfersSpaceAccess(t *testing.T) {
	d := newAPITestDB(t)
	owner := seedUser(t, d, "owner", "ownerpw12", false)
	member := seedUser(t, d, "member", "memberpw1", false)
	stranger := seedUser(t, d, "stranger", "strangerp", false)

	space := seedSpace(t, d, "Docs", "docs", owner)
	org := seedOrg(t, d, "Acme", "acme")
	seedOrgMember(t, d, org, member, orgRoleMember)
	seedOrgMember(t, d, org, stranger, orgRoleMember)
	group := seedGroup(t, d, org, "Eng")
	seedGroupMember(t, d, group, member) // member in group; stranger only in org

	if _, err := d.Exec(
		`INSERT INTO space_grants (space_id, principal_kind, principal_id, role) VALUES ($1, 'group', $2, 'editor')`,
		space, group); err != nil {
		t.Fatalf("insert group grant: %v", err)
	}

	ctx := context.Background()
	if role, err := spaceRole(ctx, d, member, space); err != nil || role != roleEditor {
		t.Fatalf("group member effective role = %q, %v; want editor", role, err)
	}
	// In the org but not the group → no access from the group grant.
	if _, err := spaceRole(ctx, d, stranger, space); err != sql.ErrNoRows {
		t.Fatalf("org-only user role err = %v; want ErrNoRows", err)
	}
}

// GET /api/spaces/{id} surfaces the caller's effective role as my_role —
// including for users who reach the space only via a group grant (absent from
// the direct members list), so clients can role-gate UI without re-deriving it.
func TestGetSpace_MyRole_GroupGrantedMember(t *testing.T) {
	d := newAPITestDB(t)
	owner := seedUser(t, d, "owner", "ownerpw12", false)
	member := seedUser(t, d, "member", "memberpw1", false)

	space := seedSpace(t, d, "Docs", "docs", owner)
	org := seedOrg(t, d, "Acme", "acme")
	seedOrgMember(t, d, org, member, orgRoleMember)
	group := seedGroup(t, d, org, "Eng")
	seedGroupMember(t, d, group, member)
	if _, err := d.Exec(
		`INSERT INTO space_grants (space_id, principal_kind, principal_id, role) VALUES ($1, 'group', $2, 'viewer')`,
		space, group); err != nil {
		t.Fatalf("insert group grant: %v", err)
	}

	srv := New(d)
	getSpace := func(u *auth.User) map[string]json.RawMessage {
		req := userRequest(http.MethodGet, "/api/spaces/"+strconv.FormatInt(space, 10), "", u)
		rec := routedRecorder("GET /api/spaces/{id}", srv.GetSpace, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("get space status = %d; want 200 (%s)", rec.Code, rec.Body)
		}
		var body struct {
			Space map[string]json.RawMessage `json:"space"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode space: %v", err)
		}
		return body.Space
	}

	if got := string(getSpace(authUser(member, "member", false))["my_role"]); got != `"viewer"` {
		t.Fatalf("group-granted member my_role = %s; want \"viewer\"", got)
	}
	if got := string(getSpace(authUser(owner, "owner", false))["my_role"]); got != `"owner"` {
		t.Fatalf("direct owner my_role = %s; want \"owner\"", got)
	}
}

// seedGroupMember satisfies the group⊆org trigger (caller must seed org
// membership first).
func seedGroupMember(t *testing.T, d *sql.DB, groupID, userID int64) {
	t.Helper()
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, groupID, userID); err != nil {
		t.Fatalf("insert group_member: %v", err)
	}
}

// Containment invariant: a non-org-member can't be added to the org's group
// (DB trigger), and leaving the org drops group membership (cascade trigger).
func TestGroupMembership_ContainmentInvariants(t *testing.T) {
	d := newAPITestDB(t)
	u := seedUser(t, d, "u", "upassword", false)
	org := seedOrg(t, d, "Acme", "acme")
	group := seedGroup(t, d, org, "Eng")

	// Not an org member yet → trigger rejects.
	if _, err := d.Exec(
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, group, u); err == nil {
		t.Fatal("adding a non-org-member to a group should be rejected by the trigger")
	}

	// Add to org, then group — now allowed.
	seedOrgMember(t, d, org, u, orgRoleMember)
	seedGroupMember(t, d, group, u)

	// Leaving the org cascades out of the group.
	if _, err := d.Exec(`DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, org, u); err != nil {
		t.Fatalf("remove org member: %v", err)
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM group_members WHERE group_id = $1 AND user_id = $2`, group, u).Scan(&n)
	if n != 0 {
		t.Fatalf("group_members after org-leave = %d; want 0 (cascade)", n)
	}
}

func TestApplyAutoJoin_EnrollsMatchingDomain(t *testing.T) {
	d := newAPITestDB(t)
	org := seedOrg(t, d, "Acme", "acme")
	if _, err := d.Exec(
		`INSERT INTO org_email_domains (domain, org_id) VALUES ('acme.com', $1)`, org); err != nil {
		t.Fatalf("seed domain: %v", err)
	}
	uid := seedUser(t, d, "carol", "carolpw12", false)

	applyAutoJoin(context.Background(), d, uid, "carol@ACME.com")

	role, err := orgRole(context.Background(), d, uid, org)
	if err != nil || role != orgRoleMember {
		t.Fatalf("auto-joined role = %q, %v; want member", role, err)
	}
	// Idempotent + non-matching domain is a no-op.
	applyAutoJoin(context.Background(), d, uid, "carol@other.com")
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM org_members WHERE user_id = $1`, uid).Scan(&n)
	if n != 1 {
		t.Fatalf("org_members count = %d; want 1", n)
	}
}
