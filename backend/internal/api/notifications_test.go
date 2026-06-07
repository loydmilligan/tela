package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"
)

func notifCount(t *testing.T, d *sql.DB, userID int64) int {
	t.Helper()
	var n int
	if err := d.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatalf("count notifications for %d: %v", userID, err)
	}
	return n
}

func TestNotifications_PageMention_GatedAndIdempotent(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	ctx := context.Background()

	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	carol := seedUser(t, d, "carol", "carolpw123", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)
	seedMember(t, d, spaceID, bob, "viewer")
	// carol is intentionally NOT a member — she must not be notified.

	body := "Hi [@Bob](tela://user/" + intStr(bob) + ") and [@Carol](tela://user/" + intStr(carol) + ")"
	au := authUser(alice, "alice", false)
	page, ae := srv.createPageCore(ctx, au, nil, pageCreateRequest{SpaceID: spaceID, Title: "Plan", Body: body})
	if ae != nil {
		t.Fatalf("create page: %v", ae)
	}

	if n := notifCount(t, d, bob); n != 1 {
		t.Fatalf("bob (mentioned member) notifications = %d, want 1", n)
	}
	if n := notifCount(t, d, carol); n != 0 {
		t.Fatalf("carol (mentioned non-member) notifications = %d, want 0", n)
	}
	if n := notifCount(t, d, alice); n != 0 {
		t.Fatalf("alice (author) notifications = %d, want 0", n)
	}

	// Re-save with a changed body that still mentions bob — must NOT duplicate.
	body2 := body + " — updated"
	if _, ae := srv.updatePageCore(ctx, au, nil, page.ID, pageUpdateRequest{Body: &body2}, false); ae != nil {
		t.Fatalf("update page: %v", ae)
	}
	if n := notifCount(t, d, bob); n != 1 {
		t.Fatalf("bob notifications after re-save = %d, want 1 (idempotent)", n)
	}
}

func TestNotifications_API_ListCountMarkRead(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	ctx := context.Background()

	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)
	seedMember(t, d, spaceID, bob, "viewer")

	body := "ping [@Bob](tela://user/" + intStr(bob) + ")"
	if _, ae := srv.createPageCore(ctx, authUser(alice, "alice", false), nil,
		pageCreateRequest{SpaceID: spaceID, Title: "Plan", Body: body}); ae != nil {
		t.Fatalf("create page: %v", ae)
	}

	bu := authUser(bob, "bob", false)

	// Unread count is 1.
	rec := routedRecorder("GET /api/notifications/unread-count", srv.UnreadNotificationCount,
		userRequest(http.MethodGet, "/api/notifications/unread-count", "", bu))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"count":1`) {
		t.Fatalf("unread-count: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// List shows the mention with its render payload, unread.
	rec = routedRecorder("GET /api/notifications", srv.ListNotifications,
		userRequest(http.MethodGet, "/api/notifications", "", bu))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: code=%d body=%q", rec.Code, rec.Body.String())
	}
	body0 := rec.Body.String()
	for _, want := range []string{`"type":"mention"`, `"actor_username":"alice"`, `"page_title":"Plan"`, `"read":false`} {
		if !strings.Contains(body0, want) {
			t.Fatalf("list missing %s: body=%q", want, body0)
		}
	}

	// Mark it read.
	var nid int64
	if err := d.QueryRowContext(ctx, `SELECT id FROM notifications WHERE user_id = $1`, bob).Scan(&nid); err != nil {
		t.Fatalf("lookup notification id: %v", err)
	}
	rec = routedRecorder("POST /api/notifications/{id}/read", srv.MarkNotificationRead,
		userRequest(http.MethodPost, "/api/notifications/"+intStr(nid)+"/read", "", bu))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("mark read: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Unread count is now 0.
	rec = routedRecorder("GET /api/notifications/unread-count", srv.UnreadNotificationCount,
		userRequest(http.MethodGet, "/api/notifications/unread-count", "", bu))
	if !strings.Contains(rec.Body.String(), `"count":0`) {
		t.Fatalf("unread-count after read: body=%q", rec.Body.String())
	}
}
