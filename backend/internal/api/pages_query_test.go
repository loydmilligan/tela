package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// queryPages POSTs to /api/pages/query and returns the decoded rows (or fails
// the test on a non-200).
func queryPages(t *testing.T, c *http.Client, tsURL, body string) []queryPageRow {
	t.Helper()
	resp, err := postJSON(c, tsURL+"/api/pages/query", body)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("query: status=%d body=%s", resp.StatusCode, b)
	}
	var env struct {
		Pages []queryPageRow `json:"pages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode query result: %v", err)
	}
	return env.Pages
}

func titlesOf(rows []queryPageRow) map[string]bool {
	m := map[string]bool{}
	for _, r := range rows {
		m[r.Title] = true
	}
	return m
}

// TestQueryPages exercises the props query endpoint: props @> containment
// matching, the space_access gate (a private space's pages are invisible to a
// non-member), the sort/limit whitelist, and the "here" space scope.
func TestQueryPages(t *testing.T) {
	ts, d := newWiredServer(t)
	owner := seedUser(t, d, "qowner", "ownerpw12", false)
	outsider := seedUser(t, d, "qoutsider", "outsiderpw12", false)

	spaceA := seedSpace(t, d, "Alpha", "alpha", owner)
	spaceB := seedSpace(t, d, "Bravo", "bravo", owner) // owner-only; outsider not a member

	oc := loginClient(t, ts, "qowner", "ownerpw12")

	// mk creates a page and returns its id.
	mk := func(spaceID int64, title, propsJSON string) int64 {
		t.Helper()
		body := fmt.Sprintf(`{"space_id":%d,"title":%q,"body":"b","props":%s}`, spaceID, title, propsJSON)
		resp, err := postJSON(oc, ts.URL+"/api/pages", body)
		if err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("create %s: status=%d body=%s", title, resp.StatusCode, b)
		}
		var env struct {
			Page struct {
				ID int64 `json:"id"`
			} `json:"page"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode create %s: %v", title, err)
		}
		return env.Page.ID
	}

	// Space A: two incidents + one doc. Space B: one incident (owner-only).
	mk(spaceA, "Incident One", `{"type":"incident","status":"active"}`)
	mk(spaceA, "Incident Two", `{"type":"incident","status":"resolved"}`)
	mk(spaceA, "A Doc", `{"type":"doc"}`)
	incBID := mk(spaceB, "Secret Incident", `{"type":"incident","status":"active"}`)

	t.Run("containment matches props @> where", func(t *testing.T) {
		rows := queryPages(t, oc, ts.URL, `{"where":{"type":"incident"}}`)
		got := titlesOf(rows)
		for _, want := range []string{"Incident One", "Incident Two", "Secret Incident"} {
			if !got[want] {
				t.Fatalf("owner query type=incident missing %q; got %v", want, got)
			}
		}
		if got["A Doc"] {
			t.Fatalf("type=incident should not match the doc: %v", got)
		}
	})

	t.Run("multi-key containment narrows", func(t *testing.T) {
		rows := queryPages(t, oc, ts.URL, `{"where":{"type":"incident","status":"active"}}`)
		got := titlesOf(rows)
		if !got["Incident One"] || got["Incident Two"] {
			t.Fatalf("type=incident+status=active want only active incidents; got %v", got)
		}
	})

	t.Run("space_access gate hides a non-member's pages", func(t *testing.T) {
		// The outsider is a member of no space → sees nothing, even matching props.
		xc := loginClient(t, ts, "qoutsider", "outsiderpw12")
		rows := queryPages(t, xc, ts.URL, `{"where":{"type":"incident"}}`)
		if len(rows) != 0 {
			t.Fatalf("outsider (no membership) must see no pages; got %d: %v", len(rows), titlesOf(rows))
		}
	})

	t.Run("space_access gate hides pages from a space the caller can't read", func(t *testing.T) {
		// Give the outsider access to Alpha only; Bravo's incident stays hidden.
		seedMember(t, d, spaceA, outsider, "viewer")
		xc := loginClient(t, ts, "qoutsider", "outsiderpw12")
		rows := queryPages(t, xc, ts.URL, `{"where":{"type":"incident"}}`)
		got := titlesOf(rows)
		if !got["Incident One"] || !got["Incident Two"] {
			t.Fatalf("Alpha viewer should see Alpha incidents; got %v", got)
		}
		if got["Secret Incident"] {
			t.Fatalf("Bravo incident leaked to a non-member of Bravo: %v", got)
		}
	})

	t.Run("space: here scopes to the block's page space", func(t *testing.T) {
		rows := queryPages(t, oc, ts.URL,
			fmt.Sprintf(`{"where":{"type":"incident"},"space":"here","page_id":%d}`, incBID))
		got := titlesOf(rows)
		if !got["Secret Incident"] {
			t.Fatalf("space=here (page in Bravo) should include Bravo's incident; got %v", got)
		}
		if got["Incident One"] {
			t.Fatalf("space=here should exclude Alpha pages; got %v", got)
		}
	})

	t.Run("space: <id> scopes to that space", func(t *testing.T) {
		rows := queryPages(t, oc, ts.URL,
			fmt.Sprintf(`{"where":{"type":"incident"},"space":%d}`, spaceA))
		got := titlesOf(rows)
		if got["Secret Incident"] {
			t.Fatalf("space=<Alpha id> should exclude Bravo pages; got %v", got)
		}
		if !got["Incident One"] {
			t.Fatalf("space=<Alpha id> should include Alpha incidents; got %v", got)
		}
	})

	t.Run("unsupported sort key is rejected (400)", func(t *testing.T) {
		resp, err := postJSON(oc, ts.URL+"/api/pages/query",
			`{"where":{"type":"incident"},"sort":"props->>'x'; DROP TABLE pages"}`)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("malicious sort: status=%d, want 400", resp.StatusCode)
		}
	})

	t.Run("whitelisted sort orders results", func(t *testing.T) {
		rows := queryPages(t, oc, ts.URL, `{"where":{"type":"incident"},"space":`+fmt.Sprintf("%d", spaceA)+`,"sort":"title"}`)
		if len(rows) < 2 {
			t.Fatalf("expected >=2 Alpha incidents, got %d", len(rows))
		}
		if rows[0].Title > rows[1].Title {
			t.Fatalf("sort=title should be ascending; got %q before %q", rows[0].Title, rows[1].Title)
		}
	})

	t.Run("limit caps the row count", func(t *testing.T) {
		rows := queryPages(t, oc, ts.URL, `{"where":{"type":"incident"},"limit":1}`)
		if len(rows) != 1 {
			t.Fatalf("limit=1 should return 1 row, got %d", len(rows))
		}
	})

	t.Run("empty where lists all readable pages", func(t *testing.T) {
		rows := queryPages(t, oc, ts.URL, `{}`)
		if len(rows) < 4 {
			t.Fatalf("empty where should list all owner-readable pages (>=4), got %d", len(rows))
		}
	})
}
