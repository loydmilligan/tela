package api

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// setUserEmail stamps an email on a seeded user (seedUser leaves it NULL).
func setUserEmail(t *testing.T, d *sql.DB, userID int64, email string) {
	t.Helper()
	if _, err := d.ExecContext(context.Background(),
		`UPDATE users SET email = $1, email_verified_at = tela_now() WHERE id = $2`, email, userID); err != nil {
		t.Fatalf("set email: %v", err)
	}
}

// waitForEmails polls the capture mailer until it has at least want messages,
// then settles briefly to catch any over-count. Notification sends are detached.
func waitForEmails(t *testing.T, cm *captureMailer, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cm.count() >= want {
			time.Sleep(60 * time.Millisecond)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d emails, got %d", want, cm.count())
}

// assertNoEmail gives the detached sender a grace window, then asserts none was
// sent beyond the baseline count.
func assertNoEmail(t *testing.T, cm *captureMailer, baseline int) {
	t.Helper()
	time.Sleep(250 * time.Millisecond)
	if got := cm.count(); got != baseline {
		t.Fatalf("expected no new email (baseline %d), got %d", baseline, got)
	}
}

func emailTo(cm *captureMailer, addr string) (string, string, bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for _, m := range cm.sent {
		if m.To == addr {
			return m.Subject, m.HTML + "\n" + m.Text, true
		}
	}
	return "", "", false
}

func newEmailServer(t *testing.T) (*Server, *sql.DB, *captureMailer) {
	t.Helper()
	d := newAPITestDB(t)
	srv := New(d)
	cm := &captureMailer{}
	srv.Mailer = cm
	return srv, d, cm
}

func TestNotificationEmail_Mention(t *testing.T) {
	srv, d, cm := newEmailServer(t)
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	setUserEmail(t, d, bob, "bob@ngss.io")
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)
	seedMember(t, d, spaceID, bob, "editor")

	body := "Planning notes — [@bob](tela://user/" + intStr(bob) + ") can you review the rollout?"
	srv.notifyPageMentions(ctx, authUser(alice, "alice", false), 42, spaceID, "Rollout", body)

	waitForEmails(t, cm, 1)
	subj, content, ok := emailTo(cm, "bob@ngss.io")
	if !ok {
		t.Fatalf("no email to bob; got %d emails", cm.count())
	}
	if !strings.Contains(subj, "alice") || !strings.Contains(subj, "Rollout") {
		t.Errorf("subject missing actor/title: %q", subj)
	}
	if !strings.Contains(content, "can you review the rollout") {
		t.Errorf("snippet not in email: %q", content)
	}
}

func TestNotificationEmail_MentionSkipsNonMemberAndSelf(t *testing.T) {
	srv, d, cm := newEmailServer(t)
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	carol := seedUser(t, d, "carol", "carolpw1234", false) // NOT a member
	setUserEmail(t, d, carol, "carol@ngss.io")
	setUserEmail(t, d, alice, "alice@ngss.io")
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)

	// Mention carol (no access) and alice (the author) — neither should be emailed.
	body := "[@carol](tela://user/" + intStr(carol) + ") and [@alice](tela://user/" + intStr(alice) + ")"
	srv.notifyPageMentions(ctx, authUser(alice, "alice", false), 7, spaceID, "Secret", body)
	assertNoEmail(t, cm, 0)
}

func TestNotificationEmail_OptOut(t *testing.T) {
	srv, d, cm := newEmailServer(t)
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	setUserEmail(t, d, bob, "bob@ngss.io")
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)
	seedMember(t, d, spaceID, bob, "editor")
	// Bob mutes the email channel for mentions (in-app still on).
	if _, err := d.ExecContext(ctx,
		`INSERT INTO notification_prefs (user_id, event_type, channel, enabled) VALUES ($1, 'mention', 'email', 0)`,
		bob); err != nil {
		t.Fatalf("insert pref: %v", err)
	}

	body := "[@bob](tela://user/" + intStr(bob) + ") fyi"
	srv.notifyPageMentions(ctx, authUser(alice, "alice", false), 9, spaceID, "Notes", body)

	assertNoEmail(t, cm, 0)
	// In-app must still fire — muting email doesn't mute the inbox.
	if n := notifCountByType(t, d, bob, notifMention); n != 1 {
		t.Fatalf("bob in-app mention = %d, want 1 (email opt-out must not affect in-app)", n)
	}
}

func TestNotificationEmail_NoAddressSkips(t *testing.T) {
	srv, d, cm := newEmailServer(t)
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false) // no email set
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)
	seedMember(t, d, spaceID, bob, "editor")

	body := "[@bob](tela://user/" + intStr(bob) + ") hi"
	srv.notifyPageMentions(ctx, authUser(alice, "alice", false), 11, spaceID, "Notes", body)

	assertNoEmail(t, cm, 0)
	if n := notifCountByType(t, d, bob, notifMention); n != 1 {
		t.Fatalf("bob in-app mention = %d, want 1", n)
	}
}

func TestNotificationEmail_CommentReply(t *testing.T) {
	srv, d, cm := newEmailServer(t)
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	setUserEmail(t, d, alice, "alice@ngss.io")
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)
	seedMember(t, d, spaceID, bob, "editor")
	page, ae := srv.createPageCore(ctx, authUser(alice, "alice", false), nil,
		pageCreateRequest{SpaceID: spaceID, Title: "Plan", Body: "hello world"})
	if ae != nil {
		t.Fatalf("create page: %v", ae)
	}
	base := cm.count() // page create may emit (author has email but isn't notified of own page)

	pre, ex, suf := "a", "b", "c"
	root, ae := srv.createCommentCore(ctx, authUser(alice, "alice", false), nil, page.ID,
		commentCreateRequest{Body: "root", AnchorPrefix: &pre, AnchorExact: &ex, AnchorSuffix: &suf})
	if ae != nil {
		t.Fatalf("root comment: %v", ae)
	}
	if _, ae := srv.createCommentCore(ctx, authUser(bob, "bob", false), nil, page.ID,
		commentCreateRequest{Body: "Good idea, shipping it", ParentID: &root.ID}); ae != nil {
		t.Fatalf("reply: %v", ae)
	}

	waitForEmails(t, cm, base+1)
	subj, content, ok := emailTo(cm, "alice@ngss.io")
	if !ok {
		t.Fatalf("no reply email to alice")
	}
	if !strings.Contains(subj, "replied") {
		t.Errorf("subject not a reply: %q", subj)
	}
	if !strings.Contains(content, "Good idea, shipping it") {
		t.Errorf("reply snippet missing: %q", content)
	}
}

func TestNotificationEmail_SpaceAdded(t *testing.T) {
	srv, d, cm := newEmailServer(t)
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	setUserEmail(t, d, bob, "bob@ngss.io")
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)
	seedMember(t, d, spaceID, bob, "editor") // bob now has access (notify reads space name)

	srv.notifySpaceAdded(ctx, authUser(alice, "alice", false), bob, spaceID)

	waitForEmails(t, cm, 1)
	subj, _, ok := emailTo(cm, "bob@ngss.io")
	if !ok || !strings.Contains(subj, "Engineering") {
		t.Fatalf("space_added email missing/space name absent: subj=%q ok=%v", subj, ok)
	}
}

func TestNotificationEmail_PageUpdatedThrottled(t *testing.T) {
	srv, d, cm := newEmailServer(t)
	ctx := context.Background()
	alice := seedUser(t, d, "alice", "alicepw123", false)
	bob := seedUser(t, d, "bob", "bobpw12345", false)
	setUserEmail(t, d, bob, "bob@ngss.io")
	spaceID := seedSpace(t, d, "Engineering", "engineering", alice)
	seedMember(t, d, spaceID, bob, "editor")
	// Bob follows the page.
	pageID := int64(101)
	if _, err := d.ExecContext(ctx,
		`INSERT INTO subscriptions (user_id, subject_kind, subject_id) VALUES ($1, 'page', $2)`,
		bob, pageID); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Two edits in quick succession → only ONE email (throttled per page/window).
	srv.notifyPageUpdate(ctx, authUser(alice, "alice", false), pageID, spaceID, "Roadmap")
	waitForEmails(t, cm, 1)
	srv.notifyPageUpdate(ctx, authUser(alice, "alice", false), pageID, spaceID, "Roadmap")
	assertNoEmail(t, cm, 1)
}
