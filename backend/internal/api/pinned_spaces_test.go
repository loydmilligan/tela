package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestPinnedSpaces_AddListDelete(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	uid := seedUser(t, d, "alice", "alicepw123", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", uid)
	u := authUser(uid, "alice", false)

	// Initially empty.
	rec := routedRecorder("GET /api/users/me/pinned-spaces",
		srv.ListPinnedSpaces, userRequest(http.MethodGet, "/api/users/me/pinned-spaces", "", u))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"space_id":`) {
		t.Fatalf("list before: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Pin it.
	rec = routedRecorder("PUT /api/spaces/{id}/pin",
		srv.AddPinnedSpace, userRequest(http.MethodPut, "/api/spaces/"+intStr(spaceID)+"/pin", "", u))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"is_pinned":true`) {
		t.Fatalf("pin: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Pin again — idempotent.
	rec = routedRecorder("PUT /api/spaces/{id}/pin",
		srv.AddPinnedSpace, userRequest(http.MethodPut, "/api/spaces/"+intStr(spaceID)+"/pin", "", u))
	if rec.Code != http.StatusOK {
		t.Fatalf("pin idempotent: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Appears in the list.
	rec = routedRecorder("GET /api/users/me/pinned-spaces",
		srv.ListPinnedSpaces, userRequest(http.MethodGet, "/api/users/me/pinned-spaces", "", u))
	if !strings.Contains(rec.Body.String(), `"space_id":`+intStr(spaceID)) {
		t.Fatalf("list missing space: body=%q", rec.Body.String())
	}

	// Unpin.
	rec = routedRecorder("DELETE /api/spaces/{id}/pin",
		srv.DeletePinnedSpace, userRequest(http.MethodDelete, "/api/spaces/"+intStr(spaceID)+"/pin", "", u))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unpin: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Gone from the list.
	rec = routedRecorder("GET /api/users/me/pinned-spaces",
		srv.ListPinnedSpaces, userRequest(http.MethodGet, "/api/users/me/pinned-spaces", "", u))
	if strings.Contains(rec.Body.String(), `"space_id":`+intStr(spaceID)) {
		t.Fatalf("pin not removed: body=%q", rec.Body.String())
	}
}

// Pinning a space you aren't a member of is 403, same as any other access denial.
func TestPinnedSpaces_NonMember_Add_Returns403(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	stranger := seedUser(t, d, "stranger", "strangerpw", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)

	rec := routedRecorder("PUT /api/spaces/{id}/pin",
		srv.AddPinnedSpace, userRequest(http.MethodPut, "/api/spaces/"+intStr(spaceID)+"/pin", "", authUser(stranger, "stranger", false)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-member pin: code=%d body=%q want 403", rec.Code, rec.Body.String())
	}
}
