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

// ?mine=1 narrows the feed to pages the caller edited, even when a teammate's
// edit is more recent and equally visible.
func TestRecentChanges_MineFiltersToOwnEdits(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	ctx := context.Background()

	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)
	seedMember(t, d, spaceID, bob, "editor")
	alicePage := seedPage(t, d, spaceID, "Alice Page")
	bobPage := seedPage(t, d, spaceID, "Bob Page")

	if _, err := insertPageRevision(ctx, d, alicePage, "a", "Alice Page", nil, &alice, "test"); err != nil {
		t.Fatalf("rev alice: %v", err)
	}
	if _, err := insertPageRevision(ctx, d, bobPage, "b", "Bob Page", nil, &bob, "test"); err != nil {
		t.Fatalf("rev bob: %v", err)
	}

	rec := routedRecorder("GET /api/recent-changes", srv.ListRecentChanges,
		userRequest(http.MethodGet, "/api/recent-changes?mine=1", "", authUser(alice, "alice", false)))
	if rec.Code != http.StatusOK {
		t.Fatalf("mine: code=%d body=%q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Alice Page") {
		t.Fatalf("mine missing own edit: %q", body)
	}
	if strings.Contains(body, "Bob Page") {
		t.Fatalf("mine leaked a teammate's edit: %q", body)
	}
}

// ?source=agent surfaces pages an agent (MCP) created or edited, and excludes
// purely-human pages — the "Changes by your AI" feed.
func TestRecentChanges_SourceAgentFilter(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	ctx := context.Background()

	alice := seedUser(t, d, "alice", "alicepw123", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)
	au := authUser(alice, "alice", false)

	// Human-created page, later edited by an agent (agentWrite=true → source=agent).
	human, ae := srv.createPageCore(ctx, au, nil, pageCreateRequest{SpaceID: spaceID, Title: "Human Page", Body: "h"})
	if ae != nil {
		t.Fatalf("human create: %v", ae)
	}
	nb := "rewritten by agent"
	if _, ae := srv.updatePageCore(ctx, au, nil, human.ID, pageUpdateRequest{Body: &nb}, true); ae != nil {
		t.Fatalf("agent update: %v", ae)
	}
	// Agent-created page (source=agent via the agent-write context).
	if _, ae := srv.createPageCore(withAgentWrite(ctx), au, nil, pageCreateRequest{SpaceID: spaceID, Title: "Agent Page", Body: "a"}); ae != nil {
		t.Fatalf("agent create: %v", ae)
	}
	// Purely-human page — must NOT show in the agent feed.
	if _, ae := srv.createPageCore(ctx, au, nil, pageCreateRequest{SpaceID: spaceID, Title: "Pure Human", Body: "p"}); ae != nil {
		t.Fatalf("pure human create: %v", ae)
	}

	rec := routedRecorder("GET /api/recent-changes", srv.ListRecentChanges,
		userRequest(http.MethodGet, "/api/recent-changes?source=agent", "", au))
	if rec.Code != http.StatusOK {
		t.Fatalf("agent feed: code=%d body=%q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Agent Page") {
		t.Fatalf("agent-created page missing from agent feed: %q", body)
	}
	if !strings.Contains(body, "Human Page") {
		t.Fatalf("agent-edited page missing from agent feed: %q", body)
	}
	if strings.Contains(body, "Pure Human") {
		t.Fatalf("purely-human page leaked into agent feed: %q", body)
	}
}
