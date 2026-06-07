package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestRecentChanges_LatestPerPage_GatedByAccess(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	ctx := context.Background()

	uid := seedUser(t, d, "alice", "alicepw123", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", uid)
	pageID := seedPage(t, d, spaceID, "Roadmap")

	// A page in a space alice can't reach — must never appear.
	otherUID := seedUser(t, d, "bob", "bobpw12345", false)
	otherSpace := seedSpace(t, d, "Secret", "secret", otherUID)
	otherPage := seedPage(t, d, otherSpace, "Hidden")

	// Two revisions on the visible page (feed should collapse to the newest)
	// and one on the hidden page.
	if _, err := insertPageRevision(ctx, d, pageID, "v1", "Roadmap", nil, &uid, "test"); err != nil {
		t.Fatalf("rev1: %v", err)
	}
	if _, err := insertPageRevision(ctx, d, pageID, "v2", "Roadmap", nil, &uid, "test"); err != nil {
		t.Fatalf("rev2: %v", err)
	}
	if _, err := insertPageRevision(ctx, d, otherPage, "h1", "Hidden", nil, &otherUID, "test"); err != nil {
		t.Fatalf("rev hidden: %v", err)
	}

	rec := routedRecorder("GET /api/recent-changes",
		srv.ListRecentChanges, userRequest(http.MethodGet, "/api/recent-changes", "", authUser(uid, "alice", false)))
	if rec.Code != http.StatusOK {
		t.Fatalf("recent-changes: code=%d body=%q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"title":"Roadmap"`) || !strings.Contains(body, `"author_username":"alice"`) {
		t.Fatalf("missing visible change: body=%q", body)
	}
	if strings.Contains(body, "Hidden") {
		t.Fatalf("leaked inaccessible page: body=%q", body)
	}
	// The page should appear exactly once despite two revisions.
	if n := strings.Count(body, `"page_id":`+intStr(pageID)); n != 1 {
		t.Fatalf("page appears %d times, want 1: body=%q", n, body)
	}
}
