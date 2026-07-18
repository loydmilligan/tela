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

// aggResult mirrors the aggregate response envelope.
type aggResult struct {
	Groups []struct {
		Key    any            `json:"key"`
		Values map[string]any `json:"values"`
	} `json:"groups"`
	SkippedNonNumeric int `json:"skipped_non_numeric"`
}

// queryAggregate POSTs an aggregate query and decodes the rollup envelope.
func queryAggregate(t *testing.T, c *http.Client, tsURL, body string) aggResult {
	t.Helper()
	resp, err := postJSON(c, tsURL+"/api/pages/query", body)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("aggregate: status=%d body=%s", resp.StatusCode, b)
	}
	var res aggResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode aggregate: %v", err)
	}
	return res
}

// groupValue finds the aggregate value for a group key (nil key = whole-set row).
func groupValue(t *testing.T, res aggResult, key any, alias string) float64 {
	t.Helper()
	for _, g := range res.Groups {
		match := (key == nil && g.Key == nil) || (g.Key != nil && fmt.Sprint(g.Key) == fmt.Sprint(key))
		if match {
			v, ok := g.Values[alias]
			if !ok || v == nil {
				t.Fatalf("group %v has no numeric %q: %v", key, alias, g.Values)
			}
			return v.(float64)
		}
	}
	t.Fatalf("no group with key %v in %v", key, res.Groups)
	return 0
}

// TestQueryPagesV2 exercises operators, sort-by-prop, and aggregation — and the
// load-bearing invariant that a private space's numeric prop never enters an
// aggregate the caller has no right to see.
func TestQueryPagesV2(t *testing.T) {
	ts, d := newWiredServer(t)
	owner := seedUser(t, d, "v2owner", "ownerpw12", false)
	outsider := seedUser(t, d, "v2out", "outpw123456", false)

	alpha := seedSpace(t, d, "Alpha", "v2alpha", owner)
	bravo := seedSpace(t, d, "Bravo", "v2bravo", owner) // owner-only

	oc := loginClient(t, ts, "v2owner", "ownerpw12")
	mk := func(spaceID int64, title, propsJSON string) {
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
	}

	// Alpha: numeric costs + a model + tags; one deliberately non-numeric cost.
	mk(alpha, "A gpt small", `{"type":"run","model":"gpt-4","cost":10,"tags":["prod","llm"]}`)
	mk(alpha, "A gpt big", `{"type":"run","model":"gpt-4","cost":20,"tags":["llm"]}`)
	mk(alpha, "A claude", `{"type":"run","model":"claude","cost":5,"tags":["prod"]}`)
	mk(alpha, "A broken cost", `{"type":"run","model":"claude","cost":"expensive"}`)
	// Bravo: a big secret cost that must never enter an outsider's rollup.
	mk(bravo, "B secret", `{"type":"run","model":"gpt-4","cost":1000}`)

	alphaScope := func(rest string) string {
		return fmt.Sprintf(`{"where":{"type":"run"},"space":%d%s}`, alpha, rest)
	}

	t.Run("operator gt filters numerically", func(t *testing.T) {
		rows := queryPages(t, oc, ts.URL, alphaScope(`,"filters":[{"key":"cost","op":"gt","value":10}]`))
		got := titlesOf(rows)
		if !got["A gpt big"] || got["A gpt small"] || got["A claude"] {
			t.Fatalf("cost>10 want only 'A gpt big'; got %v", got)
		}
	})

	t.Run("operator gte / lt / ne", func(t *testing.T) {
		gte := titlesOf(queryPages(t, oc, ts.URL, alphaScope(`,"filters":[{"key":"cost","op":"gte","value":10}]`)))
		if !gte["A gpt small"] || !gte["A gpt big"] || gte["A claude"] {
			t.Fatalf("cost>=10 want the two gpt rows; got %v", gte)
		}
		lt := titlesOf(queryPages(t, oc, ts.URL, alphaScope(`,"filters":[{"key":"cost","op":"lt","value":10}]`)))
		if !lt["A claude"] || lt["A gpt small"] {
			t.Fatalf("cost<10 want only 'A claude'; got %v", lt)
		}
		ne := titlesOf(queryPages(t, oc, ts.URL, alphaScope(`,"filters":[{"key":"model","op":"ne","value":"gpt-4"}]`)))
		if ne["A gpt small"] || ne["A gpt big"] || !ne["A claude"] {
			t.Fatalf("model!=gpt-4 want the claude rows only; got %v", ne)
		}
	})

	t.Run("operator contains on array and exists", func(t *testing.T) {
		ct := titlesOf(queryPages(t, oc, ts.URL, alphaScope(`,"filters":[{"key":"tags","op":"contains","value":"prod"}]`)))
		if !ct["A gpt small"] || !ct["A claude"] || ct["A gpt big"] {
			t.Fatalf("tags contains prod want small+claude; got %v", ct)
		}
		// 'A broken cost' has no tags → exists:tags excludes it.
		ex := titlesOf(queryPages(t, oc, ts.URL, alphaScope(`,"filters":[{"key":"tags","op":"exists"}]`)))
		if ex["A broken cost"] || !ex["A gpt small"] {
			t.Fatalf("exists:tags should include tagged rows and exclude the untagged one; got %v", ex)
		}
	})

	t.Run("sort by a prop, descending", func(t *testing.T) {
		rows := queryPages(t, oc, ts.URL, alphaScope(`,"order":[{"field":"cost","dir":"desc"}],"filters":[{"key":"cost","op":"exists"}]`))
		// Only numeric-cost rows carry a comparable value; the broken one is filtered by exists.
		var costs []float64
		for _, r := range rows {
			if c, ok := r.Props["cost"].(float64); ok {
				costs = append(costs, c)
			}
		}
		if len(costs) < 3 || costs[0] < costs[len(costs)-1] {
			t.Fatalf("cost desc should be non-increasing; got %v", costs)
		}
	})

	t.Run("aggregate SUM group by model (owner sees all spaces)", func(t *testing.T) {
		res := queryAggregate(t, oc, ts.URL,
			`{"where":{"type":"run"},"aggregate":{"fns":[{"fn":"sum","key":"cost","as":"total"}],"group_by":"model"}}`)
		if got := groupValue(t, res, "gpt-4", "total"); got != 1030 {
			t.Fatalf("SUM(cost) for gpt-4 across all readable spaces = %v, want 1030 (10+20+1000)", got)
		}
		if got := groupValue(t, res, "claude", "total"); got != 5 {
			t.Fatalf("SUM(cost) for claude = %v, want 5", got)
		}
		if res.SkippedNonNumeric != 1 {
			t.Fatalf("one non-numeric cost ('expensive') should be reported skipped; got %d", res.SkippedNonNumeric)
		}
	})

	t.Run("aggregate COUNT and whole-set (no group by)", func(t *testing.T) {
		res := queryAggregate(t, oc, ts.URL,
			`{"where":{"type":"run"},"space":`+fmt.Sprintf("%d", alpha)+`,"aggregate":{"fns":[{"fn":"count","as":"n"},{"fn":"avg","key":"cost","as":"mean"}]}}`)
		if len(res.Groups) != 1 || res.Groups[0].Key != nil {
			t.Fatalf("no group_by should yield one whole-set row with null key; got %v", res.Groups)
		}
		if got := groupValue(t, res, nil, "n"); got != 4 {
			t.Fatalf("COUNT of Alpha runs = %v, want 4", got)
		}
		// avg over the 3 numeric costs (10,20,5) = 11.666…; the broken one is excluded.
		if got := groupValue(t, res, nil, "mean"); got < 11.6 || got > 11.7 {
			t.Fatalf("AVG(cost) = %v, want ~11.67 (10+20+5)/3", got)
		}
	})

	// THE load-bearing test: a non-member's aggregate must not include a private
	// space's rows. Bravo's cost=1000 must be absent from the outsider's SUM.
	t.Run("aggregate does NOT leak a private space's rows", func(t *testing.T) {
		seedMember(t, d, alpha, outsider, "viewer") // Alpha only; NOT Bravo
		xc := loginClient(t, ts, "v2out", "outpw123456")
		res := queryAggregate(t, xc, ts.URL,
			`{"where":{"type":"run"},"aggregate":{"fns":[{"fn":"sum","key":"cost","as":"total"}]}}`)
		got := groupValue(t, res, nil, "total")
		if got != 35 {
			t.Fatalf("outsider SUM(cost) = %v, want 35 (10+20+5) — Bravo's 1000 leaked into the aggregate!", got)
		}
	})
}
