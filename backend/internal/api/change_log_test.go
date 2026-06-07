package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func changeActions(t *testing.T, d *sql.DB, spaceID int64) []string {
	t.Helper()
	rows, err := d.QueryContext(context.Background(),
		`SELECT action FROM change_log WHERE space_id = $1 ORDER BY seq`, spaceID)
	if err != nil {
		t.Fatalf("query change_log: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, a)
	}
	return out
}

// TestChangeLog_RecordsEachAction: every page mutation (create/update/move/
// delete), through the REST cores, appends the right action to the feed.
func TestChangeLog_RecordsEachAction(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "S", "s", 1)
	c := loginClient(t, ts, "admin", "adminpw12")

	resp, _ := postJSON(c, ts.URL+"/api/pages", fmt.Sprintf(`{"space_id":%d,"title":"Parent"}`, space))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create parent: %d", resp.StatusCode)
	}
	parent := decodePage(t, resp)
	resp, _ = postJSON(c, ts.URL+"/api/pages", fmt.Sprintf(`{"space_id":%d,"title":"Child"}`, space))
	child := decodePage(t, resp)

	if resp, _ := patchJSON(c, fmt.Sprintf("%s/api/pages/%d", ts.URL, child.ID), `{"body":"edited"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("update: %d", resp.StatusCode)
	}
	if resp, _ := postJSON(c, fmt.Sprintf("%s/api/pages/%d/move", ts.URL, child.ID), fmt.Sprintf(`{"parent_id":%d}`, parent.ID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("move: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/pages/%d", ts.URL, parent.ID), nil)
	if resp, _ := c.Do(req); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}

	// create×2, update, move, then delete of the parent subtree (parent + child).
	got := changeActions(t, d, space)
	want := []string{"created", "created", "updated", "moved", "deleted", "deleted"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("change_log actions = %v, want %v", got, want)
	}
}

// TestChangesEndpoint: the delta feed returns changes after a cursor, advances
// the cursor, and is membership-gated.
func TestChangesEndpoint(t *testing.T) {
	ts, d := newWiredServer(t)
	seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "S", "s", 1)
	c := loginClient(t, ts, "admin", "adminpw12")

	for i := 0; i < 3; i++ {
		postJSON(c, ts.URL+"/api/pages", fmt.Sprintf(`{"space_id":%d,"title":"P%d"}`, space, i))
	}

	// since=0 → all three, ascending, with a cursor.
	resp, err := c.Get(fmt.Sprintf("%s/api/changes?space_id=%d&since=0", ts.URL, space))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("changes: err=%v status=%d", err, resp.StatusCode)
	}
	var first struct {
		Changes []struct {
			Seq    int64  `json:"seq"`
			Action string `json:"action"`
		} `json:"changes"`
		Cursor int64 `json:"cursor"`
	}
	json.NewDecoder(resp.Body).Decode(&first)
	resp.Body.Close()
	if len(first.Changes) != 3 {
		t.Fatalf("want 3 changes, got %d", len(first.Changes))
	}
	if first.Changes[0].Seq >= first.Changes[2].Seq {
		t.Fatal("changes not ascending by seq")
	}
	if first.Cursor != first.Changes[2].Seq {
		t.Fatalf("cursor=%d, want last seq %d", first.Cursor, first.Changes[2].Seq)
	}

	// since=cursor → empty, cursor unchanged.
	resp, _ = c.Get(fmt.Sprintf("%s/api/changes?space_id=%d&since=%d", ts.URL, space, first.Cursor))
	var second struct {
		Changes []json.RawMessage `json:"changes"`
		Cursor  int64             `json:"cursor"`
	}
	json.NewDecoder(resp.Body).Decode(&second)
	resp.Body.Close()
	if len(second.Changes) != 0 {
		t.Fatalf("want 0 new changes, got %d", len(second.Changes))
	}
	if second.Cursor != first.Cursor {
		t.Fatalf("cursor advanced past last seen: %d vs %d", second.Cursor, first.Cursor)
	}

	// A non-member is forbidden.
	seedUser(t, d, "mallory", "mallorypw1", false)
	mc := loginClient(t, ts, "mallory", "mallorypw1")
	resp, _ = mc.Get(fmt.Sprintf("%s/api/changes?space_id=%d&since=0", ts.URL, space))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-member changes = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
}
