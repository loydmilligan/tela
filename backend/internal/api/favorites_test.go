package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestFavorites_AddListStatusDelete(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	uid := seedUser(t, d, "alice", "alicepw123", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", uid)
	pageID := seedPage(t, d, spaceID, "Roadmap")
	u := authUser(uid, "alice", false)

	// Initially not favorited.
	rec := routedRecorder("GET /api/pages/{id}/favorite",
		srv.GetFavoriteStatus, userRequest(http.MethodGet, "/api/pages/"+intStr(pageID)+"/favorite", "", u))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"is_favorited":false`) {
		t.Fatalf("status before: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Star it.
	rec = routedRecorder("POST /api/pages/{id}/favorite",
		srv.AddFavorite, userRequest(http.MethodPost, "/api/pages/"+intStr(pageID)+"/favorite", "", u))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"is_favorited":true`) {
		t.Fatalf("add: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Star again — idempotent.
	rec = routedRecorder("POST /api/pages/{id}/favorite",
		srv.AddFavorite, userRequest(http.MethodPost, "/api/pages/"+intStr(pageID)+"/favorite", "", u))
	if rec.Code != http.StatusOK {
		t.Fatalf("add idempotent: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Status now true.
	rec = routedRecorder("GET /api/pages/{id}/favorite",
		srv.GetFavoriteStatus, userRequest(http.MethodGet, "/api/pages/"+intStr(pageID)+"/favorite", "", u))
	if !strings.Contains(rec.Body.String(), `"is_favorited":true`) {
		t.Fatalf("status after add: body=%q", rec.Body.String())
	}

	// Appears in the list, with the space name.
	rec = routedRecorder("GET /api/users/me/favorites",
		srv.ListFavorites, userRequest(http.MethodGet, "/api/users/me/favorites", "", u))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"title":"Roadmap"`) || !strings.Contains(rec.Body.String(), `"space_name":"Engineering"`) {
		t.Fatalf("list missing page: body=%q", rec.Body.String())
	}

	// Unstar.
	rec = routedRecorder("DELETE /api/pages/{id}/favorite",
		srv.DeleteFavorite, userRequest(http.MethodDelete, "/api/pages/"+intStr(pageID)+"/favorite", "", u))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Gone from the list.
	rec = routedRecorder("GET /api/users/me/favorites",
		srv.ListFavorites, userRequest(http.MethodGet, "/api/users/me/favorites", "", u))
	if strings.Contains(rec.Body.String(), `"title":"Roadmap"`) {
		t.Fatalf("favorite not removed: body=%q", rec.Body.String())
	}
}

// A user can only favorite a page they can see — starring a page in a space
// they aren't a member of is 403, same as any other access denial.
func TestFavorites_NonMember_Add_Returns403(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	stranger := seedUser(t, d, "stranger", "strangerpw", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)
	pageID := seedPage(t, d, spaceID, "Secret")

	rec := routedRecorder("POST /api/pages/{id}/favorite",
		srv.AddFavorite, userRequest(http.MethodPost, "/api/pages/"+intStr(pageID)+"/favorite", "", authUser(stranger, "stranger", false)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-member add: code=%d body=%q want 403", rec.Code, rec.Body.String())
	}
}

// Favoriting a page that doesn't exist is 404.
func TestFavorites_MissingPage_Add_Returns404(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	uid := seedUser(t, d, "alice", "alicepw123", false)
	rec := routedRecorder("POST /api/pages/{id}/favorite",
		srv.AddFavorite, userRequest(http.MethodPost, "/api/pages/99999/favorite", "", authUser(uid, "alice", false)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing page add: code=%d body=%q want 404", rec.Code, rec.Body.String())
	}
}
