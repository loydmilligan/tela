package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"strings"
	"testing"
)

// selfHostServer wires billing + the self-host license product + an ephemeral
// signing key + a capture mailer, so the issuance flow runs end-to-end with no
// network. The minted token won't verify against the EMBEDDED public key (that's
// the vendor's real key), which is fine — these tests exercise the issuance
// plumbing (DB + email), not ee's crypto (which has its own sign/verify tests).
func selfHostServer(t *testing.T) (*Server, *sql.DB, *captureMailer) {
	t.Helper()
	s, d := wiredBillingServer(t)
	s.selfHostProductID = "prod_selfhost"
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	s.licenseSigner = priv
	cm := &captureMailer{}
	s.Mailer = cm
	return s, d, cm
}

func selfHostLicenseRow(t *testing.T, d *sql.DB, subID string) (token, status string, seats int) {
	t.Helper()
	err := d.QueryRowContext(context.Background(),
		`SELECT token, status, seats FROM selfhost_licenses WHERE polar_subscription_id = $1`, subID).
		Scan(&token, &status, &seats)
	if err != nil {
		t.Fatalf("load license row %s: %v", subID, err)
	}
	return
}

func TestSelfHostLicenseIssuedOnPurchase(t *testing.T) {
	s, d, cm := selfHostServer(t)
	ctx := context.Background()
	uid := seedUser(t, d, "buyer", "pw123456", false)
	if _, err := d.ExecContext(ctx, `UPDATE users SET email = $1 WHERE id = $2`, "buyer@example.com", uid); err != nil {
		t.Fatal(err)
	}
	ext := acctExternalID(account{Kind: accountUser, ID: uid})

	// Purchase → a key is minted, stored, and emailed. Route through reconcileBilling
	// to also prove the webhook dispatches the self-host product here (not the plan
	// reconciler — the buyer must NOT be moved off personal_free).
	e := subEvent("subscription.created", ext, "prod_selfhost", "active", false)
	e.Data.Metadata = map[string]any{"kind": "selfhost_license", "seats": "5"}
	if err := s.reconcileBilling(ctx, e); err != nil {
		t.Fatalf("issue: %v", err)
	}

	token, status, seats := selfHostLicenseRow(t, d, "sub_1")
	if !strings.HasPrefix(token, "tela_lic_") {
		t.Fatalf("minted token has wrong shape: %q", token)
	}
	if status != "active" || seats != 5 {
		t.Fatalf("row: status=%q seats=%d (want active/5)", status, seats)
	}
	// The buyer's own account plan is untouched — a self-host license is not a plan.
	if plan, _, _ := acctPlan(t, d, "users", uid); plan != "personal_free" {
		t.Fatalf("self-host purchase changed the buyer's plan to %q", plan)
	}
	// Delivery email sent to the buyer, carrying the key.
	msg, ok := cm.last()
	if !ok {
		t.Fatal("no delivery email sent")
	}
	if msg.To != "buyer@example.com" || !strings.Contains(msg.Text, token) {
		t.Fatalf("email to=%q missing key in body", msg.To)
	}
}

func TestSelfHostLicenseRenewalReissuesOnce(t *testing.T) {
	s, d, cm := selfHostServer(t)
	ctx := context.Background()
	uid := seedUser(t, d, "buyer", "pw123456", false)
	if _, err := d.ExecContext(ctx, `UPDATE users SET email = $1 WHERE id = $2`, "buyer@example.com", uid); err != nil {
		t.Fatal(err)
	}
	ext := acctExternalID(account{Kind: accountUser, ID: uid})

	e := subEvent("subscription.created", ext, "prod_selfhost", "active", false)
	e.Data.Metadata = map[string]any{"seats": "1"}
	if err := s.reconcileBilling(ctx, e); err != nil {
		t.Fatalf("issue: %v", err)
	}
	first, _, _ := selfHostLicenseRow(t, d, "sub_1")

	// A redelivery of the SAME period must not spam a second email (expiry unchanged).
	if err := s.reconcileBilling(ctx, e); err != nil {
		t.Fatalf("redeliver: %v", err)
	}
	if n := cm.count(); n != 1 {
		t.Fatalf("redelivery of same period emailed %d times (want 1)", n)
	}

	// A renewal (later period end) re-mints with a fresh expiry and re-emails.
	later := e.Data.CurrentPeriodEnd.Add(365 * 24 * 60 * 60 * 1e9) // +1y
	e2 := subEvent("subscription.updated", ext, "prod_selfhost", "active", false)
	e2.Data.CurrentPeriodEnd = &later
	e2.Data.Metadata = map[string]any{"seats": "1"}
	if err := s.reconcileBilling(ctx, e2); err != nil {
		t.Fatalf("renewal: %v", err)
	}
	renewed, _, _ := selfHostLicenseRow(t, d, "sub_1")
	if renewed == first {
		t.Fatal("renewal did not re-mint the key")
	}
	if n := cm.count(); n != 2 {
		t.Fatalf("after renewal emailed %d times (want 2)", n)
	}
}

func TestSelfHostLicenseRevoke(t *testing.T) {
	s, d, _ := selfHostServer(t)
	ctx := context.Background()
	uid := seedUser(t, d, "buyer", "pw123456", false)
	ext := acctExternalID(account{Kind: accountUser, ID: uid})

	if err := s.reconcileBilling(ctx, subEvent("subscription.created", ext, "prod_selfhost", "active", false)); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := s.reconcileBilling(ctx, subEvent("subscription.revoked", ext, "prod_selfhost", "canceled", false)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, status, _ := selfHostLicenseRow(t, d, "sub_1"); status != "revoked" {
		t.Fatalf("after revoke status=%q (want revoked)", status)
	}
}

// Without a signer configured, an event for the self-host product errors (→ 500 →
// Polar redelivers) rather than silently dropping a paid customer's key.
func TestSelfHostLicenseNoSignerErrors(t *testing.T) {
	s, d := wiredBillingServer(t)
	s.selfHostProductID = "prod_selfhost"
	// no licenseSigner set
	ctx := context.Background()
	uid := seedUser(t, d, "buyer", "pw123456", false)
	ext := acctExternalID(account{Kind: accountUser, ID: uid})

	err := s.reconcileBilling(ctx, subEvent("subscription.created", ext, "prod_selfhost", "active", false))
	if err == nil {
		t.Fatal("expected an error when the signing key is missing")
	}
}
