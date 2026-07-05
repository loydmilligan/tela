package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// decodeLicensePayload cracks a minted token's payload (base64url JSON, short
// keys) WITHOUT verifying the signature — enough to assert the minted contents.
func decodeLicensePayload(t *testing.T, token string) map[string]any {
	t.Helper()
	raw := strings.TrimPrefix(token, "tela_lic_")
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 {
		t.Fatalf("malformed token: %q", token)
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return m
}

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

func TestSelfHostLicenseMintContent(t *testing.T) {
	s, _, _ := selfHostServer(t)
	exp := time.Now().Add(365 * 24 * time.Hour)
	tok, err := s.mintSelfHostLicense("acme@example.com", 7, exp, "refh-abc")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	m := decodeLicensePayload(t, tok)
	if m["tier"] != "enterprise" {
		t.Fatalf("tier=%v (want enterprise)", m["tier"])
	}
	if m["lid"] != "refh-abc" {
		t.Fatalf("lid=%v (want refh-abc)", m["lid"])
	}
	if m["cust"] != "acme@example.com" {
		t.Fatalf("cust=%v", m["cust"])
	}
	if seats, _ := m["seats"].(float64); int(seats) != 7 {
		t.Fatalf("seats=%v (want 7)", m["seats"])
	}
	feat, _ := m["feat"].(map[string]any)
	if feat["*"] != true {
		t.Fatalf("feat=%v (want all-features \"*\")", m["feat"])
	}
	if e, _ := m["exp"].(float64); int64(e) != exp.Unix() {
		t.Fatalf("exp=%v (want %d)", m["exp"], exp.Unix())
	}
}

func TestSelfHostLicenseCanceledMarksRow(t *testing.T) {
	s, d, _ := selfHostServer(t)
	ctx := context.Background()
	uid := seedUser(t, d, "buyer", "pw123456", false)
	ext := acctExternalID(account{Kind: accountUser, ID: uid})
	if err := s.reconcileBilling(ctx, subEvent("subscription.created", ext, "prod_selfhost", "active", false)); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := s.reconcileBilling(ctx, subEvent("subscription.canceled", ext, "prod_selfhost", "active", true)); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, status, _ := selfHostLicenseRow(t, d, "sub_1"); status != "canceled" {
		t.Fatalf("after cancel status=%q (want canceled)", status)
	}
}

// A revoke/cancel event that omits product_id must still reach license handling
// (routed by subscription id), or the row wrongly stays Active. Covers the
// isSelfHostSubscription fallback.
func TestSelfHostLicenseRoutesBySubIdWhenProductMissing(t *testing.T) {
	s, d, _ := selfHostServer(t)
	ctx := context.Background()
	uid := seedUser(t, d, "buyer", "pw123456", false)
	ext := acctExternalID(account{Kind: accountUser, ID: uid})
	if err := s.reconcileBilling(ctx, subEvent("subscription.created", ext, "prod_selfhost", "active", false)); err != nil {
		t.Fatalf("issue: %v", err)
	}
	revoke := subEvent("subscription.revoked", ext, "", "canceled", false) // product_id empty
	if err := s.reconcileBilling(ctx, revoke); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, status, _ := selfHostLicenseRow(t, d, "sub_1"); status != "revoked" {
		t.Fatalf("revoke without product_id left status=%q (want revoked)", status)
	}
}

// A redelivery within the same period must NOT re-mint — the stored key must stay
// byte-identical to the one the buyer was emailed/installed.
func TestSelfHostLicenseSamePeriodTokenStable(t *testing.T) {
	s, d, _ := selfHostServer(t)
	ctx := context.Background()
	uid := seedUser(t, d, "buyer", "pw123456", false)
	ext := acctExternalID(account{Kind: accountUser, ID: uid})
	e := subEvent("subscription.created", ext, "prod_selfhost", "active", false)
	if err := s.reconcileBilling(ctx, e); err != nil {
		t.Fatalf("issue: %v", err)
	}
	first, _, _ := selfHostLicenseRow(t, d, "sub_1")
	// A same-period subscription.updated redelivery.
	if err := s.reconcileBilling(ctx, subEvent("subscription.updated", ext, "prod_selfhost", "active", false)); err != nil {
		t.Fatalf("update: %v", err)
	}
	if again, _, _ := selfHostLicenseRow(t, d, "sub_1"); again != first {
		t.Fatal("same-period update re-minted the key (should stay stable)")
	}
}

// The refresh handle is set on issue and stays STABLE across a renewal (the key's
// lid must survive re-minting so the instance can always find its subscription).
func TestSelfHostLicenseRefreshIdStable(t *testing.T) {
	s, d, _ := selfHostServer(t)
	ctx := context.Background()
	uid := seedUser(t, d, "buyer", "pw123456", false)
	ext := acctExternalID(account{Kind: accountUser, ID: uid})
	e := subEvent("subscription.created", ext, "prod_selfhost", "active", false)
	if err := s.reconcileBilling(ctx, e); err != nil {
		t.Fatalf("issue: %v", err)
	}
	var rid1 string
	if err := d.QueryRowContext(ctx, `SELECT refresh_id FROM selfhost_licenses WHERE polar_subscription_id='sub_1'`).Scan(&rid1); err != nil || rid1 == "" {
		t.Fatalf("refresh_id not set on issue: %q err=%v", rid1, err)
	}
	later := e.Data.CurrentPeriodEnd.Add(365 * 24 * 60 * 60 * 1e9)
	e2 := subEvent("subscription.updated", ext, "prod_selfhost", "active", false)
	e2.Data.CurrentPeriodEnd = &later
	if err := s.reconcileBilling(ctx, e2); err != nil {
		t.Fatalf("renewal: %v", err)
	}
	var rid2 string
	_ = d.QueryRowContext(ctx, `SELECT refresh_id FROM selfhost_licenses WHERE polar_subscription_id='sub_1'`).Scan(&rid2)
	if rid2 != rid1 {
		t.Fatalf("refresh_id changed on renewal: %q -> %q", rid1, rid2)
	}
}

// The public refresh endpoint rejects a missing token (400) and any junk /
// unsigned token (401) — it only ever answers a validly-signed key.
func TestRefreshEndpointRejectsJunk(t *testing.T) {
	s, _, _ := selfHostServer(t)
	for _, tc := range []struct {
		q    string
		code int
	}{
		{"", 400},
		{"?token=not-a-key", 401},
		{"?token=tela_lic_deadbeef.deadbeef", 401},
	} {
		req := httptest.NewRequest("GET", "/api/public/license/refresh"+tc.q, nil)
		rr := httptest.NewRecorder()
		s.RefreshSelfHostLicense(rr, req)
		if rr.Code != tc.code {
			t.Fatalf("token=%q → %d (want %d)", tc.q, rr.Code, tc.code)
		}
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
