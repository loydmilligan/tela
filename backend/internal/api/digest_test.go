package api

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zcag/tela/backend/internal/mailer"
)

// countingMailer records how many emails were "sent".
type countingMailer struct{ n int32 }

func (m *countingMailer) Send(_ context.Context, _ mailer.Message) error {
	atomic.AddInt32(&m.n, 1)
	return nil
}

// enableWeeklyDigest opts a user in with a verified email so they're a send
// candidate, and clears any prior send stamp.
func enableWeeklyDigest(t *testing.T, d *sql.DB, userID int64, email string) {
	t.Helper()
	if _, err := d.Exec(
		`UPDATE users SET digest_frequency='weekly', email=$1,
		        email_verified_at=tela_now(), digest_last_sent_at='' WHERE id=$2`,
		email, userID); err != nil {
		t.Fatalf("enable weekly: %v", err)
	}
}

func TestSendDueDigests_NoDuplicateAcrossReruns(t *testing.T) {
	_, d, srv := newWiredServerOnDiskWithSrv(t)
	fake := &countingMailer{}
	srv.Mailer = fake
	alice := seedUser(t, d, "alice", "alicepw12", false)
	space := seedSpace(t, d, "Alice", "alice-s", alice)
	seedPageInSpace(t, d, space, nil, "Fresh page", "some content") // this-week activity
	enableWeeklyDigest(t, d, alice, "alice@example.com")

	ctx := context.Background()
	// First run: exactly one send.
	if n, err := srv.SendDueDigests(ctx, false); err != nil || n != 1 {
		t.Fatalf("first run: sent=%d err=%v (want 1)", n, err)
	}
	// Rerun immediately (simulates a redeploy tick): already claimed → no send.
	if n, err := srv.SendDueDigests(ctx, false); err != nil || n != 0 {
		t.Fatalf("rerun: sent=%d err=%v (want 0)", n, err)
	}
	if got := atomic.LoadInt32(&fake.n); got != 1 {
		t.Fatalf("mailer fired %d times across reruns, want exactly 1", got)
	}
}

func TestSendDueDigests_ConcurrentSingleSend(t *testing.T) {
	_, d, srv := newWiredServerOnDiskWithSrv(t)
	fake := &countingMailer{}
	srv.Mailer = fake
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	space := seedSpace(t, d, "Bob", "bob-s", bob)
	seedPageInSpace(t, d, space, nil, "Fresh page", "some content")
	enableWeeklyDigest(t, d, bob, "bob@example.com")

	// Five concurrent runs (advisory lock + atomic claim) → exactly one send.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = srv.SendDueDigests(context.Background(), false) }()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&fake.n); got != 1 {
		t.Fatalf("concurrent runs fired the mailer %d times, want exactly 1", got)
	}
}

// A crash between claim and send must NOT resend: once stamped, the user is not
// due again, so the next run skips them (they miss this week rather than double).
func TestSendDueDigests_StampBeforeSendPreventsResend(t *testing.T) {
	_, d, srv := newWiredServerOnDiskWithSrv(t)
	carol := seedUser(t, d, "carol", "carolpw12", false)
	space := seedSpace(t, d, "Carol", "carol-s", carol)
	seedPageInSpace(t, d, space, nil, "Fresh page", "some content")
	enableWeeklyDigest(t, d, carol, "carol@example.com")

	// Simulate "sent but crashed before... " by having the mailer fail: the claim
	// still commits, so a rerun must not send.
	srv.Mailer = failMailer{}
	if _, err := srv.SendDueDigests(context.Background(), false); err != nil {
		t.Fatalf("run with failing mailer: %v", err)
	}
	// Now a healthy mailer on rerun: user was already claimed → no resend.
	fake := &countingMailer{}
	srv.Mailer = fake
	if n, _ := srv.SendDueDigests(context.Background(), false); n != 0 {
		t.Fatalf("rerun after a failed send resent %d (want 0 — miss, not duplicate)", n)
	}
	if got := atomic.LoadInt32(&fake.n); got != 0 {
		t.Fatalf("resent after failed send %d times, want 0", got)
	}
}

type failMailer struct{}

func (failMailer) Send(_ context.Context, _ mailer.Message) error {
	return context.DeadlineExceeded
}

func setDigestLastSent(t *testing.T, d *sql.DB, userID int64, ts string) {
	t.Helper()
	if _, err := d.Exec(`UPDATE users SET digest_last_sent_at = $1 WHERE id = $2`, ts, userID); err != nil {
		t.Fatalf("set last_sent: %v", err)
	}
}

// A recipient's link base is their org's active custom domain when they have
// one, else the canonical host; a pending (unverified) domain never counts, and
// the earliest-created active hostname wins a tie.
func TestUserDigestBase(t *testing.T) {
	_, d, srv := newWiredServerOnDiskWithSrv(t)
	ctx := context.Background()

	mkOrg := func(name, slug string) int64 {
		var id int64
		if err := d.QueryRow(`INSERT INTO orgs (name, slug) VALUES ($1,$2) RETURNING id`, name, slug).Scan(&id); err != nil {
			t.Fatalf("seed org: %v", err)
		}
		return id
	}
	join := func(userID, orgID int64) {
		if _, err := d.Exec(`INSERT INTO org_members (org_id, user_id, org_role) VALUES ($1,$2,'member')`, orgID, userID); err != nil {
			t.Fatalf("join org: %v", err)
		}
	}
	host := func(orgID int64, hostname, status, createdAt string) {
		if _, err := d.Exec(
			`INSERT INTO org_hostnames (hostname, org_id, status, verify_token, created_at) VALUES ($1,$2,$3,'tok',$4)`,
			hostname, orgID, status, createdAt); err != nil {
			t.Fatalf("seed hostname: %v", err)
		}
	}

	// No org → canonical host (empty in tests, TELA_PUBLIC_BASE_URL unset).
	loner := seedUser(t, d, "loner", "lonerpw12", false)
	if got := srv.userDigestBase(ctx, loner); got != canonicalBaseURL() {
		t.Fatalf("no-org base = %q, want canonical %q", got, canonicalBaseURL())
	}

	// Org with only a PENDING domain → still canonical (a pending host can't
	// serve the app, so it must not scope links).
	pend := seedUser(t, d, "pend", "pendpw123", false)
	po := mkOrg("Pending Org", "pending-org")
	join(pend, po)
	host(po, "pending.example.com", "pending", "2026-01-01 00:00:00")
	if got := srv.userDigestBase(ctx, pend); got != canonicalBaseURL() {
		t.Fatalf("pending-domain base = %q, want canonical %q", got, canonicalBaseURL())
	}

	// Org with an ACTIVE domain → that white-label host.
	member := seedUser(t, d, "member", "memberpw1", false)
	ao := mkOrg("Active Org", "active-org")
	join(member, ao)
	host(ao, "wiki.acme.test", "active", "2026-02-01 00:00:00")
	if got := srv.userDigestBase(ctx, member); got != "https://wiki.acme.test" {
		t.Fatalf("active-domain base = %q, want https://wiki.acme.test", got)
	}

	// A second, later active hostname on the same org must not win: earliest
	// created is the canonical white-label host.
	host(ao, "aaa.acme.test", "active", "2026-03-01 00:00:00")
	if got := srv.userDigestBase(ctx, member); got != "https://wiki.acme.test" {
		t.Fatalf("multi-domain base = %q, want earliest https://wiki.acme.test", got)
	}
}

// The anchor must always be a Monday 05:00 UTC, at/before now, within the past
// week — swept across two weeks so the Monday-before/after-05:00 boundary is hit.
func TestDigestWeekAnchor(t *testing.T) {
	start := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	for h := 0; h < 24*14; h++ {
		now := start.Add(time.Duration(h) * time.Hour)
		a := digestWeekAnchor(now)
		if a.Weekday() != time.Monday || a.Hour() != 5 || a.Minute() != 0 {
			t.Fatalf("now=%s → anchor=%s is not Monday 05:00", now, a)
		}
		if a.After(now) {
			t.Fatalf("now=%s → anchor=%s is in the future", now, a)
		}
		if now.Sub(a) >= 7*24*time.Hour {
			t.Fatalf("now=%s → anchor=%s is more than a week back", now, a)
		}
	}
}

// The weekly gate: a user already sent since this week's Monday anchor is NOT due;
// one last sent before it (i.e. last week) IS due. Uses the real anchor so it's
// timezone/clock-honest.
func TestSendDueDigests_MondayWeeklyGate(t *testing.T) {
	_, d, srv := newWiredServerOnDiskWithSrv(t)
	fake := &countingMailer{}
	srv.Mailer = fake
	anchor := digestWeekAnchor(time.Now().UTC())

	// A: sent AFTER this week's anchor → already got it → not due.
	a := seedUser(t, d, "amy", "amypw1234", false)
	sa := seedSpace(t, d, "Amy", "amy-s", a)
	seedPageInSpace(t, d, sa, nil, "Fresh", "content")
	enableWeeklyDigest(t, d, a, "amy@example.com")
	setDigestLastSent(t, d, a, anchor.Add(time.Hour).Format(tsLayout))

	// B: sent BEFORE the anchor (last week) → due now.
	b := seedUser(t, d, "ben", "benpw1234", false)
	sb := seedSpace(t, d, "Ben", "ben-s", b)
	seedPageInSpace(t, d, sb, nil, "Fresh", "content")
	enableWeeklyDigest(t, d, b, "ben@example.com")
	setDigestLastSent(t, d, b, anchor.Add(-time.Hour).Format(tsLayout))

	n, err := srv.SendDueDigests(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sent=%d, want 1 (only Ben is due this week)", n)
	}
	if got := atomic.LoadInt32(&fake.n); got != 1 {
		t.Fatalf("mailer fired %d, want 1", got)
	}
	// And a rerun the same week sends nobody (both now stamped ≥ anchor).
	if n, _ := srv.SendDueDigests(context.Background(), false); n != 0 {
		t.Fatalf("same-week rerun sent %d, want 0", n)
	}
}
