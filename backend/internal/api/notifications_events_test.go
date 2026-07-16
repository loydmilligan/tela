package api

import (
	"context"
	"net/http"
	"testing"
)

func TestNotifications_SpaceAdded(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)

	rec := routedRecorder("POST /api/spaces/{id}/members", srv.AddSpaceMember,
		userRequest(http.MethodPost, "/api/spaces/"+intStr(spaceID)+"/members",
			`{"username":"bob","role":"editor"}`, authUser(alice, "alice", false)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("add member: code=%d body=%q", rec.Code, rec.Body.String())
	}

	if n := notifCountByType(t, d, bob, notifSpaceAdded); n != 1 {
		t.Fatalf("bob space_added = %d, want 1", n)
	}
	var spaceName string
	if err := d.QueryRowContext(context.Background(),
		`SELECT data->>'space_name' FROM notifications WHERE user_id = $1 AND type = 'space_added'`,
		bob).Scan(&spaceName); err != nil {
		t.Fatalf("read space_added data: %v", err)
	}
	if spaceName != "Engineering" {
		t.Fatalf("space_added space_name = %q, want Engineering", spaceName)
	}
	// The actor (alice) is not notified about her own action.
	if n := notifCountByType(t, d, alice, notifSpaceAdded); n != 0 {
		t.Fatalf("alice (actor) space_added = %d, want 0", n)
	}
}

func TestNotifications_CommentReply(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)
	seedMember(t, d, spaceID, bob, "editor")
	page, ae := srv.createPageCore(ctx, authUser(alice, "alice", false), nil,
		pageCreateRequest{SpaceID: spaceID, Title: "Plan", Body: "hello world"}, true)
	if ae != nil {
		t.Fatalf("create page: %v", ae)
	}

	// alice posts a root comment (needs the anchor triple).
	pre, ex, suf := "a", "b", "c"
	root, ae := srv.createCommentCore(ctx, authUser(alice, "alice", false), nil, page.ID,
		commentCreateRequest{Body: "root", AnchorPrefix: &pre, AnchorExact: &ex, AnchorSuffix: &suf})
	if ae != nil {
		t.Fatalf("root comment: %v", ae)
	}

	// bob replies → alice (root author) is notified.
	if _, ae := srv.createCommentCore(ctx, authUser(bob, "bob", false), nil, page.ID,
		commentCreateRequest{Body: "reply", ParentID: &root.ID}); ae != nil {
		t.Fatalf("reply: %v", ae)
	}
	if n := notifCountByType(t, d, alice, notifCommentReply); n != 1 {
		t.Fatalf("alice comment_reply = %d, want 1", n)
	}

	// alice replying to her own comment must not notify herself.
	if _, ae := srv.createCommentCore(ctx, authUser(alice, "alice", false), nil, page.ID,
		commentCreateRequest{Body: "self", ParentID: &root.ID}); ae != nil {
		t.Fatalf("self reply: %v", ae)
	}
	if n := notifCountByType(t, d, alice, notifCommentReply); n != 1 {
		t.Fatalf("self-reply notified; alice comment_reply = %d, want 1", n)
	}
}

func TestNotifications_PageComment(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)
	seedMember(t, d, spaceID, bob, "editor")
	page, ae := srv.createPageCore(ctx, authUser(alice, "alice", false), nil,
		pageCreateRequest{SpaceID: spaceID, Title: "Plan", Body: "hello world"}, true)
	if ae != nil {
		t.Fatalf("create page: %v", ae)
	}
	// alice follows the page explicitly (don't depend on autowatch defaults).
	if _, err := d.ExecContext(ctx,
		`INSERT INTO subscriptions (user_id, subject_kind, subject_id) VALUES ($1, 'page', $2)
		 ON CONFLICT DO NOTHING`, alice, page.ID); err != nil {
		t.Fatalf("subscribe alice: %v", err)
	}

	pre, ex, suf := "a", "b", "c"
	// bob leaves a root comment → alice (follower) is notified; bob (author) is not.
	root, ae := srv.createCommentCore(ctx, authUser(bob, "bob", false), nil, page.ID,
		commentCreateRequest{Body: "nice note", AnchorPrefix: &pre, AnchorExact: &ex, AnchorSuffix: &suf})
	if ae != nil {
		t.Fatalf("bob root comment: %v", ae)
	}
	if n := notifCountByType(t, d, alice, notifPageComment); n != 1 {
		t.Fatalf("alice page_comment = %d, want 1", n)
	}
	if n := notifCountByType(t, d, bob, notifPageComment); n != 0 {
		t.Fatalf("bob (commenter) page_comment = %d, want 0", n)
	}

	// bob replies to his own comment → alice, still a follower and not the parent
	// author, gets another page_comment (count 2). No comment_reply (self-reply).
	if _, ae := srv.createCommentCore(ctx, authUser(bob, "bob", false), nil, page.ID,
		commentCreateRequest{Body: "one more thing", ParentID: &root.ID}); ae != nil {
		t.Fatalf("bob reply: %v", ae)
	}
	if n := notifCountByType(t, d, alice, notifPageComment); n != 2 {
		t.Fatalf("alice page_comment after reply = %d, want 2", n)
	}

	// alice roots her own comment (she's the commenter → excluded, stays 2), then
	// bob replies to it. alice gets comment_reply, but NOT a duplicate page_comment
	// for that reply (she's the parent author → alsoExcluded).
	aRoot, ae := srv.createCommentCore(ctx, authUser(alice, "alice", false), nil, page.ID,
		commentCreateRequest{Body: "question?", AnchorPrefix: &pre, AnchorExact: &ex, AnchorSuffix: &suf})
	if ae != nil {
		t.Fatalf("alice root comment: %v", ae)
	}
	if n := notifCountByType(t, d, alice, notifPageComment); n != 2 {
		t.Fatalf("alice page_comment after own root = %d, want 2 (self excluded)", n)
	}
	if _, ae := srv.createCommentCore(ctx, authUser(bob, "bob", false), nil, page.ID,
		commentCreateRequest{Body: "answer", ParentID: &aRoot.ID}); ae != nil {
		t.Fatalf("bob reply to alice: %v", ae)
	}
	if n := notifCountByType(t, d, alice, notifCommentReply); n != 1 {
		t.Fatalf("alice comment_reply = %d, want 1", n)
	}
	if n := notifCountByType(t, d, alice, notifPageComment); n != 2 {
		t.Fatalf("alice page_comment after bob reply-to-her = %d, want 2 (no dup with comment_reply)", n)
	}
}
