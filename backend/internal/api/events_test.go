package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func countEvents(t *testing.T, d *sql.DB, typ string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM events WHERE type = $1`, typ).Scan(&n); err != nil {
		t.Fatalf("count events %q: %v", typ, err)
	}
	return n
}

// A bad login records auth.login_failed; a good one records auth.login. Exercised
// end-to-end through the real Login handler + middleware (the wired server).
func TestEvents_AuthLoginAndFailure(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "alice", "alicepw12", false)

	// Wrong password → failed event, no success event yet.
	resp, err := http.Post(ts.URL+"/api/auth/login", "application/json",
		strings.NewReader(`{"username":"alice","password":"wrongpass"}`))
	if err != nil {
		t.Fatalf("bad login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login status=%d want 401", resp.StatusCode)
	}
	if got := countEvents(t, d, evtAuthLoginFailed); got != 1 {
		t.Fatalf("auth.login_failed=%d want 1", got)
	}
	if got := countEvents(t, d, evtAuthLogin); got != 0 {
		t.Fatalf("auth.login=%d want 0 before success", got)
	}

	// Correct password (via loginClient) → success event. Also assert the failed
	// row captured the attempted identifier in detail.
	_ = loginClient(t, ts, "alice", "alicepw12")
	if got := countEvents(t, d, evtAuthLogin); got != 1 {
		t.Fatalf("auth.login=%d want 1", got)
	}
	var detail string
	if err := d.QueryRow(`SELECT detail FROM events WHERE type=$1 LIMIT 1`, evtAuthLoginFailed).Scan(&detail); err != nil {
		t.Fatalf("read failed-login detail: %v", err)
	}
	if !strings.Contains(detail, "alice") {
		t.Fatalf("failed-login detail %q missing attempted identifier", detail)
	}
}

// The page-view beacon records page.view scoped to the page; a create + edit
// through the HTTP API record page.create / page.edit.
func TestEvents_PageLifecycle(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	seedSpace(t, d, "Engineering", "engineering", admin)
	c := loginClient(t, ts, "admin", "adminpw12")

	// Resolve the space id the admin owns.
	var spaceID int64
	if err := d.QueryRow(`SELECT id FROM spaces WHERE slug='engineering'`).Scan(&spaceID); err != nil {
		t.Fatalf("space id: %v", err)
	}

	// Create a page → page.create.
	resp, err := c.Post(ts.URL+"/api/pages", "application/json",
		strings.NewReader(fmt.Sprintf(`{"space_id":%d,"parent_id":null,"title":"Runbook","body":"hello"}`, spaceID)))
	if err != nil {
		t.Fatalf("create page: %v", err)
	}
	var created struct {
		Page struct {
			ID int64 `json:"id"`
		} `json:"page"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	resp.Body.Close()
	pageID := created.Page.ID
	if got := countEvents(t, d, evtPageCreate); got != 1 {
		t.Fatalf("page.create=%d want 1", got)
	}

	// Edit it → page.edit.
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+fmt.Sprintf("/api/pages/%d", pageID),
		strings.NewReader(`{"body":"hello world, expanded"}`))
	req.Header.Set("Content-Type", "application/json")
	if resp, err = c.Do(req); err != nil {
		t.Fatalf("edit page: %v", err)
	}
	resp.Body.Close()
	if got := countEvents(t, d, evtPageEdit); got != 1 {
		t.Fatalf("page.edit=%d want 1", got)
	}

	// View beacon → page.view (204).
	resp, err = c.Post(ts.URL+fmt.Sprintf("/api/pages/%d/view", pageID), "application/json", nil)
	if err != nil {
		t.Fatalf("view beacon: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("view beacon status=%d want 204", resp.StatusCode)
	}
	var viewTarget sql.NullInt64
	if err := d.QueryRow(`SELECT target_id FROM events WHERE type=$1 LIMIT 1`, evtPageView).Scan(&viewTarget); err != nil {
		t.Fatalf("read page.view: %v", err)
	}
	if !viewTarget.Valid || viewTarget.Int64 != pageID {
		t.Fatalf("page.view target=%v want %d", viewTarget, pageID)
	}
}

// The Events feed hides instance-admin activity by default (operator noise) and
// re-includes it with ?include_admins=1. Anonymous (NULL-actor) rows are always
// kept — they can't be an admin.
func TestEvents_HideAdminActivityByDefault(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw123", true)
	bobID := seedUser(t, d, "bob", "bobpw1234", false)
	ctx := context.Background()

	recordEvent(ctx, d, eventInput{Type: evtPageView, ActorUserID: &adminID, ActorLabel: "admin", TargetLabel: "A"})
	recordEvent(ctx, d, eventInput{Type: evtPageView, ActorUserID: &bobID, ActorLabel: "bob", TargetLabel: "B"})
	recordEvent(ctx, d, eventInput{Type: evtPageView, ActorLabel: "anon", TargetLabel: "C"}) // NULL actor

	list := func(qs string) []eventDTO {
		t.Helper()
		rec := routedRecorder("GET /api/admin/events", srv.ListEvents,
			userRequest(http.MethodGet, "/api/admin/events?types=page.view"+qs, "", authUser(adminID, "admin", true)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}
		var out struct {
			Events []eventDTO `json:"events"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v body=%q", err, rec.Body.String())
		}
		return out.Events
	}

	// Default: admin's page.view is dropped; bob + anonymous remain.
	def := list("")
	if len(def) != 2 {
		t.Fatalf("default events=%d want 2 (admin hidden): %+v", len(def), def)
	}
	for _, e := range def {
		if e.ActorUserID != nil && *e.ActorUserID == adminID {
			t.Fatalf("admin activity leaked into default view: %+v", e)
		}
	}

	// include_admins=1 → all three.
	if all := list("&include_admins=1"); len(all) != 3 {
		t.Fatalf("include_admins events=%d want 3: %+v", len(all), all)
	}
}

// ListEvents is instance-admin-only and honors the types filter + the keyset
// `before` cursor.
func TestEvents_ListGatedAndFiltered(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	adminID := seedUser(t, d, "admin", "adminpw123", true)
	userID := seedUser(t, d, "bob", "bobpw1234", false)
	ctx := context.Background()

	// Seed a mix of events directly.
	for i := 0; i < 3; i++ {
		recordEvent(ctx, d, eventInput{Type: evtAuthLogin, ActorLabel: "bob"})
	}
	recordEvent(ctx, d, eventInput{Type: evtPageView, ActorLabel: "bob", TargetLabel: "Runbook"})

	// Non-admin → 403.
	rec := routedRecorder("GET /api/admin/events", srv.ListEvents,
		userRequest(http.MethodGet, "/api/admin/events", "", authUser(userID, "bob", false)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin status=%d want 403", rec.Code)
	}

	// Admin, filtered to auth.login → exactly the 3 seeded.
	rec = routedRecorder("GET /api/admin/events", srv.ListEvents,
		userRequest(http.MethodGet, "/api/admin/events?types=auth.login", "", authUser(adminID, "admin", true)))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status=%d body=%q", rec.Code, rec.Body.String())
	}
	var out struct {
		Events []struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"events"`
		NextCursor *int64 `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%q", err, rec.Body.String())
	}
	if len(out.Events) != 3 {
		t.Fatalf("filtered events=%d want 3", len(out.Events))
	}
	for _, e := range out.Events {
		if e.Type != evtAuthLogin {
			t.Fatalf("types filter leaked %q", e.Type)
		}
	}

	// Keyset: `before` the lowest returned id should drop everything (ids are
	// monotonic and these are the only auth.login rows).
	lowest := out.Events[len(out.Events)-1].ID
	rec = routedRecorder("GET /api/admin/events", srv.ListEvents,
		userRequest(http.MethodGet, fmt.Sprintf("/api/admin/events?types=auth.login&before=%d", lowest), "", authUser(adminID, "admin", true)))
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Events) != 0 {
		t.Fatalf("before-cursor returned %d want 0", len(out.Events))
	}
}
