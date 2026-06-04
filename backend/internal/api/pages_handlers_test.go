package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"
)

// seedPage inserts a top-level page row directly and returns the new id.
func seedPage(t *testing.T, d *sql.DB, spaceID int64, title string) int64 {
	t.Helper()
	var id int64
	err := d.QueryRowContext(context.Background(),
		`INSERT INTO pages(space_id, parent_id, title, body, position) VALUES ($1, NULL, $2, '', 0) RETURNING id`,
		spaceID, title).Scan(&id)
	if err != nil {
		t.Fatalf("insert page %q: %v", title, err)
	}
	return id
}

// M6.7d — pages.go info-leak fix: non-members must not be able to
// distinguish "page exists in a space I'm not in" from "page does not
// exist". Both states collapse to 403 forbidden "not a member".

func TestGetPage_NonMember_MissingPage_Returns403(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	stranger := seedUser(t, d, "stranger", "strangerpw", false)
	seedSpace(t, d, "Engineering", "engineering", owner)

	req := userRequest(http.MethodGet, "/api/pages/99999", "",
		authUser(stranger, "stranger", false))
	rec := routedRecorder("GET /api/pages/{id}", srv.GetPage, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q want 403", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("missing code=forbidden: body=%q", rec.Body.String())
	}
}

func TestGetPage_NonMember_ExistingPageInAnotherSpace_Returns403(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	stranger := seedUser(t, d, "stranger", "strangerpw", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)
	pageID := seedPage(t, d, spaceID, "Secret Page")

	req := userRequest(http.MethodGet, "/api/pages/"+intStr(pageID), "",
		authUser(stranger, "stranger", false))
	rec := routedRecorder("GET /api/pages/{id}", srv.GetPage, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q want 403", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("missing code=forbidden: body=%q", rec.Body.String())
	}
	// And the title must not leak.
	if strings.Contains(rec.Body.String(), "Secret Page") {
		t.Fatalf("page title leaked in 403 body: %q", rec.Body.String())
	}
}

func TestGetPage_Member_OwnSpacePage_Returns200(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)
	pageID := seedPage(t, d, spaceID, "Visible Page")

	req := userRequest(http.MethodGet, "/api/pages/"+intStr(pageID), "",
		authUser(owner, "owner", false))
	rec := routedRecorder("GET /api/pages/{id}", srv.GetPage, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q want 200", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"title":"Visible Page"`) {
		t.Fatalf("missing title in body: %q", rec.Body.String())
	}
}

// Members are not given a special path for missing pages because at the
// request level we cannot prove what space the would-have-been page lived
// in — so even members get a 403 for genuinely missing ids. The only 404s
// from these handlers come out of the inner mutation race window (row
// disappeared between our SELECT and the UPDATE/DELETE inside the same tx).
func TestGetPage_Member_MissingPage_Returns403(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	seedSpace(t, d, "Engineering", "engineering", owner)

	req := userRequest(http.MethodGet, "/api/pages/99999", "",
		authUser(owner, "owner", false))
	rec := routedRecorder("GET /api/pages/{id}", srv.GetPage, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q want 403", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("missing code=forbidden: body=%q", rec.Body.String())
	}
}

// Regression coverage for the other 4 handlers that share the same SELECT-
// page-then-check-membership shape. Only the non-member-missing case (the
// behaviour change) is checked here; the rest of each handler is exercised
// by existing tests.

func TestBacklinks_NonMember_MissingPage_Returns403(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	stranger := seedUser(t, d, "stranger", "strangerpw", false)
	seedSpace(t, d, "Engineering", "engineering", owner)

	req := userRequest(http.MethodGet, "/api/pages/99999/backlinks", "",
		authUser(stranger, "stranger", false))
	rec := routedRecorder("GET /api/pages/{id}/backlinks", srv.Backlinks, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q want 403", rec.Code, rec.Body.String())
	}
}

func TestBacklinks_NonMember_ExistingPageInAnotherSpace_Returns403(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	stranger := seedUser(t, d, "stranger", "strangerpw", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", owner)
	pageID := seedPage(t, d, spaceID, "Target Page")

	req := userRequest(http.MethodGet, "/api/pages/"+intStr(pageID)+"/backlinks", "",
		authUser(stranger, "stranger", false))
	rec := routedRecorder("GET /api/pages/{id}/backlinks", srv.Backlinks, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q want 403", rec.Code, rec.Body.String())
	}
}

func TestUpdatePage_NonMember_MissingPage_Returns403(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	stranger := seedUser(t, d, "stranger", "strangerpw", false)
	seedSpace(t, d, "Engineering", "engineering", owner)

	req := userRequest(http.MethodPatch, "/api/pages/99999", `{"title":"x"}`,
		authUser(stranger, "stranger", false))
	rec := routedRecorder("PATCH /api/pages/{id}", srv.UpdatePage, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q want 403", rec.Code, rec.Body.String())
	}
}

func TestDeletePage_NonMember_MissingPage_Returns403(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	stranger := seedUser(t, d, "stranger", "strangerpw", false)
	seedSpace(t, d, "Engineering", "engineering", owner)

	req := userRequest(http.MethodDelete, "/api/pages/99999", "",
		authUser(stranger, "stranger", false))
	rec := routedRecorder("DELETE /api/pages/{id}", srv.DeletePage, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q want 403", rec.Code, rec.Body.String())
	}
}

func TestMovePage_NonMember_MissingPage_Returns403(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw123", false)
	stranger := seedUser(t, d, "stranger", "strangerpw", false)
	seedSpace(t, d, "Engineering", "engineering", owner)

	req := userRequest(http.MethodPost, "/api/pages/99999/move", `{"position":0}`,
		authUser(stranger, "stranger", false))
	rec := routedRecorder("POST /api/pages/{id}/move", srv.MovePage, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q want 403", rec.Code, rec.Body.String())
	}
}
