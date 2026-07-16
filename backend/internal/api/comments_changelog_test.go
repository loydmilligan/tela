package api

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// The headline: a save records a changelog entry that notifies NOBODY. Without
// the System path this would reach every follower over in-app + email + ntfy
// phone push on every edit — the reason §4 was blocked.
func TestChangelog_AutoCommentIsSilentAndDebounced(t *testing.T) {
	ts, d := newWiredServer(t)
	author := seedUser(t, d, "cluser", "clpw123456", false)
	follower := seedUser(t, d, "clfollower", "flpw123456", false)
	space := seedSpace(t, d, "Docs", "docs-cl", author)
	seedMember(t, d, space, follower, "editor")

	page := seedPropsPage(t, d, space, "Runbook", `{}`)
	// follower watches the page — they are exactly who would get spammed.
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO subscriptions (user_id, subject_kind, subject_id) VALUES ($1, 'page', $2)`,
		follower, page); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	ctx := context.Background()
	ac := loginClient(t, ts, "cluser", "clpw123456")

	countNotifs := func(typ string) int {
		t.Helper()
		var n int
		if err := d.QueryRowContext(ctx,
			`SELECT count(*) FROM notifications WHERE user_id = $1 AND type = $2`,
			follower, typ).Scan(&n); err != nil {
			t.Fatalf("count notifications: %v", err)
		}
		return n
	}
	changelog := func() []map[string]any {
		t.Helper()
		rows, err := d.QueryContext(ctx,
			`SELECT props FROM comments WHERE page_id = $1 AND deleted_at IS NULL
			   AND props->>'type' = 'change' ORDER BY id`, page)
		if err != nil {
			t.Fatalf("read changelog: %v", err)
		}
		defer rows.Close()
		var out []map[string]any
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				t.Fatalf("scan: %v", err)
			}
			m := map[string]any{}
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("decode props: %v", err)
			}
			out = append(out, m)
		}
		return out
	}

	save := func(body string) {
		t.Helper()
		bodyJSON, _ := json.Marshal(body)
		r, err := patchJSON(ac, fmt.Sprintf("%s/api/pages/%d", ts.URL, page),
			fmt.Sprintf(`{"body":%s}`, bodyJSON))
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("save: status=%d", r.StatusCode)
		}
	}

	save("first edit")

	entries := changelog()
	if len(entries) != 1 {
		t.Fatalf("want 1 changelog entry after a save, got %d", len(entries))
	}
	if entries[0]["change_summary"] == nil {
		t.Errorf("entry carries no change_summary: %#v", entries[0])
	}
	// The key it must NOT use — that name belongs to the page abstract.
	if _, clash := entries[0]["summary"]; clash {
		t.Errorf("changelog used 'summary' — reserved for the page abstract: %#v", entries[0])
	}
	if entries[0]["auto"] != true {
		t.Errorf("entry not marked auto: %#v", entries[0])
	}

	// THE POINT: the follower was told about the page edit, but NOT about the
	// changelog comment. page_comment stays at zero no matter how much we save.
	if got := countNotifs("page_comment"); got != 0 {
		t.Fatalf("auto change-comment notified the follower %d time(s) — this is the push-spam the System path exists to prevent", got)
	}

	// Debounce: a flurry collapses into the one open entry.
	save("second edit")
	save("third edit")
	entries = changelog()
	if len(entries) != 1 {
		t.Fatalf("3 saves in the debounce window should collapse to 1 entry, got %d", len(entries))
	}
	if entries[0]["edits"] != float64(3) {
		t.Errorf("want edits=3 after collapsing, got %#v", entries[0]["edits"])
	}
	// Amended entries carry the datetime override (created_at is the base).
	if entries[0]["datetime"] == nil {
		t.Errorf("amended entry should carry a datetime override: %#v", entries[0])
	}
	if got := countNotifs("page_comment"); got != 0 {
		t.Fatalf("still must not notify after %d saves", 3)
	}

	// A REAL comment on the same page still notifies — silence is scoped to the
	// system path, not broken for everyone.
	if _, err := postJSON(ac, fmt.Sprintf("%s/api/pages/%d/comments", ts.URL, page),
		`{"body":"a real human comment","anchor_prefix":"","anchor_exact":"first","anchor_suffix":""}`); err != nil {
		t.Fatalf("post real comment: %v", err)
	}
	if got := countNotifs("page_comment"); got != 1 {
		t.Fatalf("a human comment must still notify the follower; got %d", got)
	}
}
