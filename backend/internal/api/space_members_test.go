package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestListSpaceMembers_AnyMemberCanRead(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	viewer := seedUser(t, d, "viewer", "viewerpw123", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)
	seedMember(t, d, spaceID, viewer, "viewer")

	req := userRequest(http.MethodGet, "/api/spaces/"+intStr(spaceID)+"/members", "",
		authUser(viewer, "viewer", false))
	rec := routedRecorder("GET /api/spaces/{id}/members", srv.ListSpaceMembers, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q want 200", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"owner"`) || !strings.Contains(body, `"viewer"`) {
		t.Fatalf("body missing roles: %q", body)
	}
}

func TestListSpaceMembers_NonMemberForbidden(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	stranger := seedUser(t, d, "stranger", "strangerpw", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)

	req := userRequest(http.MethodGet, "/api/spaces/"+intStr(spaceID)+"/members", "",
		authUser(stranger, "stranger", false))
	rec := routedRecorder("GET /api/spaces/{id}/members", srv.ListSpaceMembers, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q want 403", rec.Code, rec.Body.String())
	}
}

func TestAddSpaceMember_OwnerCanAdd(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)

	req := userRequest(http.MethodPost, "/api/spaces/"+intStr(spaceID)+"/members",
		`{"username":"bob","role":"editor"}`,
		authUser(owner, "owner", false))
	rec := routedRecorder("POST /api/spaces/{id}/members", srv.AddSpaceMember, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%q want 201", rec.Code, rec.Body.String())
	}
	// verify membership row exists.
	var role string
	if err := d.QueryRowContext(context.Background(),
		`SELECT role FROM space_members WHERE space_id = ? AND user_id = ?`,
		spaceID, bob).Scan(&role); err != nil {
		t.Fatalf("lookup new member: %v", err)
	}
	if role != "editor" {
		t.Fatalf("role=%q, want editor", role)
	}
}

func TestAddSpaceMember_EditorCannotAdd(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	editor := seedUser(t, d, "editor", "editorpw123", false)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)
	seedMember(t, d, spaceID, editor, "editor")

	req := userRequest(http.MethodPost, "/api/spaces/"+intStr(spaceID)+"/members",
		`{"username":"bob","role":"viewer"}`,
		authUser(editor, "editor", false))
	rec := routedRecorder("POST /api/spaces/{id}/members", srv.AddSpaceMember, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q want 403", rec.Code, rec.Body.String())
	}
	_ = bob
}

func TestAddSpaceMember_DuplicateConflict(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)
	seedMember(t, d, spaceID, bob, "viewer")

	req := userRequest(http.MethodPost, "/api/spaces/"+intStr(spaceID)+"/members",
		`{"username":"bob","role":"editor"}`,
		authUser(owner, "owner", false))
	rec := routedRecorder("POST /api/spaces/{id}/members", srv.AddSpaceMember, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%q want 409", rec.Code, rec.Body.String())
	}
}

func TestAddSpaceMember_InvalidRole(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	seedUser(t, d, "bob", "bobpw1234", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)

	req := userRequest(http.MethodPost, "/api/spaces/"+intStr(spaceID)+"/members",
		`{"username":"bob","role":"superuser"}`,
		authUser(owner, "owner", false))
	rec := routedRecorder("POST /api/spaces/{id}/members", srv.AddSpaceMember, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%q want 400", rec.Code, rec.Body.String())
	}
}

func TestPatchSpaceMember_LastOwnerDemoteRejected(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)

	req := userRequest(http.MethodPatch,
		"/api/spaces/"+intStr(spaceID)+"/members/"+intStr(owner),
		`{"role":"viewer"}`,
		authUser(owner, "owner", false))
	rec := routedRecorder("PATCH /api/spaces/{id}/members/{user_id}", srv.PatchSpaceMember, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%q want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"last_owner"`) {
		t.Fatalf("missing last_owner code: %q", rec.Body.String())
	}
}

func TestPatchSpaceMember_PromoteAndDemoteWithSecondOwner(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner1 := seedUser(t, d, "owner-1", "ownerpw123", false)
	owner2 := seedUser(t, d, "owner-2", "ownerpw123", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner1)
	seedMember(t, d, spaceID, owner2, "owner")

	// owner-1 demotes owner-2 to editor — ok, owner-1 still an owner.
	req := userRequest(http.MethodPatch,
		"/api/spaces/"+intStr(spaceID)+"/members/"+intStr(owner2),
		`{"role":"editor"}`,
		authUser(owner1, "owner-1", false))
	rec := routedRecorder("PATCH /api/spaces/{id}/members/{user_id}", srv.PatchSpaceMember, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q want 200", rec.Code, rec.Body.String())
	}
}

func TestDeleteSpaceMember_SelfLeaveAllowedForNonOwner(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	viewer := seedUser(t, d, "viewer", "viewerpw123", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)
	seedMember(t, d, spaceID, viewer, "viewer")

	req := userRequest(http.MethodDelete,
		"/api/spaces/"+intStr(spaceID)+"/members/"+intStr(viewer), "",
		authUser(viewer, "viewer", false))
	rec := routedRecorder("DELETE /api/spaces/{id}/members/{user_id}", srv.DeleteSpaceMember, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%q want 204", rec.Code, rec.Body.String())
	}
}

func TestDeleteSpaceMember_LastOwnerSelfLeaveRejected(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)

	req := userRequest(http.MethodDelete,
		"/api/spaces/"+intStr(spaceID)+"/members/"+intStr(owner), "",
		authUser(owner, "owner", false))
	rec := routedRecorder("DELETE /api/spaces/{id}/members/{user_id}", srv.DeleteSpaceMember, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%q want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"last_owner"`) {
		t.Fatalf("missing last_owner: %q", rec.Body.String())
	}
}

func TestDeleteSpaceMember_NonOwnerCannotRemoveOthers(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	editor := seedUser(t, d, "editor", "editorpw123", false)
	viewer := seedUser(t, d, "viewer", "viewerpw123", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)
	seedMember(t, d, spaceID, editor, "editor")
	seedMember(t, d, spaceID, viewer, "viewer")

	req := userRequest(http.MethodDelete,
		"/api/spaces/"+intStr(spaceID)+"/members/"+intStr(viewer), "",
		authUser(editor, "editor", false))
	rec := routedRecorder("DELETE /api/spaces/{id}/members/{user_id}", srv.DeleteSpaceMember, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q want 403", rec.Code, rec.Body.String())
	}
}
