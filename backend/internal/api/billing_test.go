package api

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/zcag/tela/backend/internal/billing"
	"github.com/zcag/tela/backend/internal/testdb"
)

// wiredBillingServer builds a Server with Polar mapped to two products so the
// reconciler can resolve product → plan. No network is touched — reconcileBilling
// only reads the product map + writes the DB.
func wiredBillingServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	d := testdb.New(t)
	s := New(d)
	s.billing = billing.New(billing.Config{
		Token:         "test_token",
		WebhookSecret: "test_secret",
		Products: map[string]string{
			"personal_plus":      "prod_plus",
			"personal_plus@year": "prod_plus_yearly",
			"org_team":           "prod_team",
		},
	})
	return s, d
}

func acctPlan(t *testing.T, d *sql.DB, table string, id int64) (planKey, status string, cancel int) {
	t.Helper()
	if err := d.QueryRowContext(context.Background(),
		`SELECT plan_key, subscription_status, subscription_cancel_at_period_end FROM `+table+` WHERE id = $1`, id).
		Scan(&planKey, &status, &cancel); err != nil {
		t.Fatalf("load %s %d: %v", table, id, err)
	}
	return
}

func subEvent(typ, extID, product, status string, cancelAtEnd bool) billing.Event {
	end := time.Now().Add(30 * 24 * time.Hour)
	e := billing.Event{Type: typ}
	e.Data.ID = "sub_1"
	e.Data.Status = status
	e.Data.ProductID = product
	e.Data.CustomerID = "cus_1"
	e.Data.CurrentPeriodEnd = &end
	e.Data.CancelAtPeriodEnd = cancelAtEnd
	e.Data.Customer.ExternalID = extID
	return e
}

func TestReconcileUserSubscriptionLifecycle(t *testing.T) {
	s, d := wiredBillingServer(t)
	ctx := context.Background()
	uid := seedUser(t, d, "alice", "pw123456", false)
	ext := acctExternalID(account{Kind: accountUser, ID: uid})

	// Activate → user is on personal_plus, trial cleared.
	if err := s.reconcileBilling(ctx, subEvent("subscription.active", ext, "prod_plus", "active", false)); err != nil {
		t.Fatalf("active: %v", err)
	}
	if plan, status, _ := acctPlan(t, d, "users", uid); plan != "personal_plus" || status != "active" {
		t.Fatalf("after active: plan=%q status=%q", plan, status)
	}
	var trialEnds sql.NullString
	if err := d.QueryRowContext(ctx, `SELECT trial_ends_at FROM users WHERE id=$1`, uid).Scan(&trialEnds); err != nil {
		t.Fatal(err)
	}
	if trialEnds.Valid {
		t.Fatalf("paid plan should clear the trial, got trial_ends_at=%q", trialEnds.String)
	}
	var custID sql.NullString
	if err := d.QueryRowContext(ctx, `SELECT polar_customer_id FROM users WHERE id=$1`, uid).Scan(&custID); err != nil {
		t.Fatal(err)
	}
	if custID.String != "cus_1" {
		t.Fatalf("customer id not stored: %q", custID.String)
	}

	// Cancel scheduled → still on plus, flagged cancel-at-period-end.
	if err := s.reconcileBilling(ctx, subEvent("subscription.canceled", ext, "prod_plus", "active", true)); err != nil {
		t.Fatalf("canceled: %v", err)
	}
	if plan, _, cancel := acctPlan(t, d, "users", uid); plan != "personal_plus" || cancel != 1 {
		t.Fatalf("after cancel-scheduled: plan=%q cancel=%d (should keep plan, flag cancel)", plan, cancel)
	}

	// Revoked → downgrade to personal_free.
	if err := s.reconcileBilling(ctx, subEvent("subscription.revoked", ext, "prod_plus", "canceled", false)); err != nil {
		t.Fatalf("revoked: %v", err)
	}
	if plan, status, cancel := acctPlan(t, d, "users", uid); plan != "personal_free" || cancel != 0 || status != "canceled" {
		t.Fatalf("after revoke: plan=%q status=%q cancel=%d (should downgrade)", plan, status, cancel)
	}
}

func TestReconcileOrgViaMetadataFallback(t *testing.T) {
	s, d := wiredBillingServer(t)
	ctx := context.Background()
	orgID := seedOrg(t, d, "Acme", "acme")

	// External id absent → resolve from metadata we set at checkout.
	e := subEvent("subscription.active", "", "prod_team", "active", false)
	e.Data.Metadata = map[string]any{"account_kind": "org", "account_id": float64(orgID)}
	if err := s.reconcileBilling(ctx, e); err != nil {
		t.Fatalf("org active: %v", err)
	}
	if plan, status, _ := acctPlan(t, d, "orgs", orgID); plan != "org_team" || status != "active" {
		t.Fatalf("org after active: plan=%q status=%q", plan, status)
	}
}

func TestReconcileUnmappedProductIsNoop(t *testing.T) {
	s, d := wiredBillingServer(t)
	ctx := context.Background()
	uid := seedUser(t, d, "bob", "pw123456", false)
	ext := acctExternalID(account{Kind: accountUser, ID: uid})

	if err := s.reconcileBilling(ctx, subEvent("subscription.active", ext, "prod_unknown", "active", false)); err != nil {
		t.Fatalf("unmapped product should be a no-op, got: %v", err)
	}
	if plan, _, _ := acctPlan(t, d, "users", uid); plan != "personal_free" {
		t.Fatalf("unmapped product changed plan to %q", plan)
	}
}

// A subscription on the YEARLY product grants the same tier as monthly — the
// reconciler resolves product → plan ignoring cadence.
func TestReconcileYearlyProductGrantsSameTier(t *testing.T) {
	s, d := wiredBillingServer(t)
	ctx := context.Background()
	uid := seedUser(t, d, "carol", "pw123456", false)
	ext := acctExternalID(account{Kind: accountUser, ID: uid})

	if err := s.reconcileBilling(ctx, subEvent("subscription.active", ext, "prod_plus_yearly", "active", false)); err != nil {
		t.Fatalf("yearly active: %v", err)
	}
	if plan, status, _ := acctPlan(t, d, "users", uid); plan != "personal_plus" || status != "active" {
		t.Fatalf("yearly product should grant personal_plus, got plan=%q status=%q", plan, status)
	}
}
