package api

import (
	"context"
	"net/http"
	"testing"
)

// The monthly LLM cap enforces atomically: calls succeed up to the cap, then
// 402; an unlimited tier (NULL cap) is never metered.
func TestCheckAndRecordLLMCall_MonthlyCap(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	uid := seedUser(t, d, "capped", "cappedpw12", false)
	// Pin a tiny cap on the plan this account holds (isolated test DB).
	if _, err := d.Exec(`UPDATE plans SET max_llm_calls_per_month=2 WHERE key='personal_plus'`); err != nil {
		t.Fatalf("set cap: %v", err)
	}
	if _, err := d.Exec(`UPDATE users SET plan_key='personal_plus' WHERE id=$1`, uid); err != nil {
		t.Fatalf("set plan: %v", err)
	}
	ctx := context.Background()
	acct := account{Kind: accountUser, ID: uid}

	if ae := srv.checkAndRecordLLMCall(ctx, acct); ae != nil {
		t.Fatalf("call 1 should pass: %+v", ae)
	}
	if ae := srv.checkAndRecordLLMCall(ctx, acct); ae != nil {
		t.Fatalf("call 2 should pass: %+v", ae)
	}
	ae := srv.checkAndRecordLLMCall(ctx, acct)
	if ae == nil || ae.Status != http.StatusPaymentRequired {
		t.Fatalf("call 3 should be 402 quota_exceeded, got %+v", ae)
	}

	// Move to the unlimited tier → never metered.
	if _, err := d.Exec(`UPDATE users SET plan_key='personal_unlimited' WHERE id=$1`, uid); err != nil {
		t.Fatalf("set unlimited: %v", err)
	}
	if ae := srv.checkAndRecordLLMCall(ctx, acct); ae != nil {
		t.Fatalf("unlimited tier should never cap: %+v", ae)
	}
}
