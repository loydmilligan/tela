package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// TestAdminStats_Aggregates — seeds activity and asserts the dashboard payload
// reflects it: totals, the daily view series, the top-pages leaderboard, and the
// ask answer-rate.
func TestAdminStats_Aggregates(t *testing.T) {
	ts, d := newWiredServer(t)
	ctx := context.Background()
	admin := seedUser(t, d, "admin", "testpass123", true)
	spaceID := seedSpace(t, d, "Docs", "docs", admin)
	pageID := seedPage(t, d, spaceID, "Guide")

	// 5 views + 2 edits today, by the admin.
	for i := 0; i < 5; i++ {
		if _, err := d.ExecContext(ctx,
			`INSERT INTO events (type, target_kind, target_id, actor_user_id, actor_label)
			 VALUES ('page.view','page',$1,$2,'admin')`, pageID, admin); err != nil {
			t.Fatalf("insert view: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := d.ExecContext(ctx,
			`INSERT INTO events (type, target_kind, target_id, actor_user_id, actor_label)
			 VALUES ('page.edit','page',$1,$2,'admin')`, pageID, admin); err != nil {
			t.Fatalf("insert edit: %v", err)
		}
	}
	// An ask that retrieved nothing + one that did.
	if _, err := d.ExecContext(ctx,
		`INSERT INTO ask_log (user_id, question, answered) VALUES ($1,'q1',0),($1,'q2',1)`, admin); err != nil {
		t.Fatalf("insert ask_log: %v", err)
	}

	c := loginClient(t, ts, "admin", "testpass123")
	resp, err := c.Get(ts.URL + "/api/admin/stats")
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var got adminStats
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Pages < 1 || got.Users < 1 || got.Spaces < 1 {
		t.Fatalf("totals look empty: %+v", got)
	}
	if len(got.Days) != statsWindowDays || len(got.Views) != statsWindowDays {
		t.Fatalf("series length wrong: days=%d views=%d", len(got.Days), len(got.Views))
	}
	// Today is the last bucket; the 5 views landed there.
	if got.Views[statsWindowDays-1] != 5 {
		t.Fatalf("today's views=%d want 5", got.Views[statsWindowDays-1])
	}
	if got.Edits[statsWindowDays-1] != 2 {
		t.Fatalf("today's edits=%d want 2", got.Edits[statsWindowDays-1])
	}
	if len(got.TopPages) != 1 || got.TopPages[0].PageID != pageID || got.TopPages[0].Count != 5 {
		t.Fatalf("top pages wrong: %+v", got.TopPages)
	}
	if len(got.TopContributors) != 1 || got.TopContributors[0].Label != "admin" || got.TopContributors[0].Count != 2 {
		t.Fatalf("top contributors wrong: %+v", got.TopContributors)
	}
	if got.Asks30 != 2 || got.AsksAnswered30 != 1 {
		t.Fatalf("asks=%d answered=%d want 2/1", got.Asks30, got.AsksAnswered30)
	}
	// Active users: the admin acted today → DAU ≥ 1.
	if got.DAU < 1 {
		t.Fatalf("DAU=%d want ≥1", got.DAU)
	}
	// Cumulative growth ends at the current totals.
	if got.UsersCum[statsWindowDays-1] != got.Users {
		t.Fatalf("users_cum tail=%d want %d", got.UsersCum[statsWindowDays-1], got.Users)
	}
}

// TestAdminStats_AdminOnly — non-admins are forbidden.
func TestAdminStats_AdminOnly(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "bob", "testpass123", false)
	c := loginClient(t, ts, "bob", "testpass123")
	resp, err := c.Get(ts.URL + "/api/admin/stats")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want 403", resp.StatusCode)
	}
}
