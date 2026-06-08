package api

import (
	"context"
	"testing"
)

// A future trial resolves to the paid tier (with its feature flags); once the
// trial window passes, planFor falls back to the base plan — no job, no write.
func TestPlanFor_TrialResolvesThenDowngrades(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	uid := seedUser(t, d, "trialer", "trialerpw1", false)
	ctx := context.Background()
	acct := account{Kind: accountUser, ID: uid}

	if _, err := d.Exec(`UPDATE users SET trial_plan_key='personal_plus',
		trial_ends_at=to_char((now() AT TIME ZONE 'UTC') + interval '30 days','YYYY-MM-DD HH24:MI:SS')
		WHERE id=$1`, uid); err != nil {
		t.Fatalf("set trial: %v", err)
	}
	p, err := planFor(ctx, d, acct)
	if err != nil {
		t.Fatalf("planFor (trial): %v", err)
	}
	if p.Key != "personal_plus" {
		t.Fatalf("trial effective plan = %q; want personal_plus", p.Key)
	}
	if !srv.featureEnabled(ctx, acct, "managed_rag") {
		t.Fatal("managed_rag should be enabled during the trial")
	}

	// Expire the trial → downgrade to the base plan, feature gone.
	if _, err := d.Exec(`UPDATE users SET trial_ends_at='2000-01-01 00:00:00' WHERE id=$1`, uid); err != nil {
		t.Fatalf("expire trial: %v", err)
	}
	p2, err := planFor(ctx, d, acct)
	if err != nil {
		t.Fatalf("planFor (expired): %v", err)
	}
	if p2.Key != "personal_free" {
		t.Fatalf("post-trial effective plan = %q; want personal_free", p2.Key)
	}
	if srv.featureEnabled(ctx, acct, "managed_rag") {
		t.Fatal("managed_rag should be gone after the trial expires")
	}
}
