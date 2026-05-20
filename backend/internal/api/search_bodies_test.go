package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
)

// searchBodiesResp mirrors the JSON contract returned by GET /api/search/bodies.
type searchBodiesResp struct {
	Results []searchBodyHit `json:"results"`
}

// TestSearchBodies_FullFlow exercises the happy path + every documented error
// envelope via the wired stack: missing/blank inputs, membership gating,
// silent limit clamp, score normalisation, tie-break ordering, and the FTS5
// injection defence.
func TestSearchBodies_FullFlow(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	_ = seedUser(t, d, "eve", "evepw12345", false) // non-member
	space := seedSpace(t, d, "Test Space", "test-space", admin)
	seedMember(t, d, space, bob, roleViewer)

	adminC := loginClient(t, ts, "admin", "adminpw12")
	bobC := loginClient(t, ts, "bob", "bobpw1234")
	eveC := loginClient(t, ts, "eve", "evepw12345")

	base := ts.URL + "/api/search/bodies"

	// 1. Missing space_id → 400 bad_request.
	resp, _ := adminC.Get(base + "?q=foo")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"bad_request"`) {
		t.Fatalf("missing space_id: status=%d body=%s want 400 bad_request", resp.StatusCode, body)
	}

	// 2. Bad space_id → 400 bad_request.
	resp, _ = adminC.Get(base + "?space_id=abc&q=foo")
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"bad_request"`) {
		t.Fatalf("bad space_id: status=%d body=%s want 400 bad_request", resp.StatusCode, body)
	}

	// 3. Missing q → 400 invalid_query.
	resp, _ = adminC.Get(base + fmt.Sprintf("?space_id=%d", space))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"invalid_query"`) {
		t.Fatalf("missing q: status=%d body=%s want 400 invalid_query", resp.StatusCode, body)
	}

	// 4. Whitespace q → 400 invalid_query.
	resp, _ = adminC.Get(base + fmt.Sprintf("?space_id=%d&q=%%20%%20", space))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"invalid_query"`) {
		t.Fatalf("whitespace q: status=%d body=%s want 400 invalid_query", resp.StatusCode, body)
	}

	// 5. space_not_found.
	resp, _ = adminC.Get(base + "?space_id=99999&q=foo")
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), `"code":"space_not_found"`) {
		t.Fatalf("space_not_found: status=%d body=%s want 404 space_not_found", resp.StatusCode, body)
	}

	// 6. Non-member eve → 403 forbidden.
	resp, _ = eveC.Get(base + fmt.Sprintf("?space_id=%d&q=foo", space))
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"code":"forbidden"`) {
		t.Fatalf("non-member: status=%d body=%s want 403 forbidden", resp.StatusCode, body)
	}

	// 7. Empty space → 200, results=[].
	got := getBodySearch(t, adminC, base+fmt.Sprintf("?space_id=%d&q=anything", space))
	if len(got.Results) != 0 {
		t.Fatalf("empty space: got %d results, want 0", len(got.Results))
	}

	// Seed pages with distinct text content so we can assert ordering by
	// match quality. The pages_fts triggers populate the index automatically.
	pageIDs := map[string]int64{
		"alpha":    seedPageInSpace(t, d, space, nil, "Alpha Page", "alpha alpha alpha widget content"),
		"bravo":    seedPageInSpace(t, d, space, nil, "Bravo Page", "alpha widget once"),
		"charlie":  seedPageInSpace(t, d, space, nil, "Charlie Page", "completely unrelated body text"),
		"titleOnly": seedPageInSpace(t, d, space, nil, "Alpha Stuff", "no widget in this body"),
	}

	// 8. Happy path: admin queries "alpha widget" and gets ranked hits.
	got = getBodySearch(t, adminC, base+fmt.Sprintf("?space_id=%d&q=alpha+widget", space))
	if len(got.Results) < 2 {
		t.Fatalf("alpha widget: got %d results, want >=2", len(got.Results))
	}
	// Top hit should be the alpha-heavy page.
	if got.Results[0].ID != pageIDs["alpha"] {
		t.Fatalf("top hit id=%d, want alpha page id=%d (results=%+v)",
			got.Results[0].ID, pageIDs["alpha"], got.Results)
	}
	// Scores must be in [0,1), monotonically non-increasing (we ORDER BY bm25 ASC).
	for i, r := range got.Results {
		if r.Score < 0 || r.Score >= 1 {
			t.Fatalf("result %d score=%f out of [0,1)", i, r.Score)
		}
		if i > 0 && r.Score > got.Results[i-1].Score+1e-9 {
			t.Fatalf("result %d score=%f > result %d score=%f — not monotonic",
				i, r.Score, i-1, got.Results[i-1].Score)
		}
	}

	// 9. Title-only match returned (FTS5 indexes both columns).
	got = getBodySearch(t, adminC, base+fmt.Sprintf("?space_id=%d&q=Stuff", space))
	foundTitleOnly := false
	for _, r := range got.Results {
		if r.ID == pageIDs["titleOnly"] {
			foundTitleOnly = true
		}
	}
	if !foundTitleOnly {
		t.Fatalf("expected title-only match for q=Stuff, got results=%+v", got.Results)
	}

	// 10. Viewer bob can read body search (viewer-OK gate).
	got = getBodySearch(t, bobC, base+fmt.Sprintf("?space_id=%d&q=alpha", space))
	if len(got.Results) == 0 {
		t.Fatalf("viewer bob got 0 results for q=alpha, want >=1")
	}

	// 11. Silent limit clamp (NOT a 400). limit=9999 returns at most maxLimit
	//     rows; we only seeded 4 here so we just verify it's a 200 and the
	//     count is bounded.
	got = getBodySearch(t, adminC, base+fmt.Sprintf("?space_id=%d&q=alpha&limit=9999", space))
	if len(got.Results) > searchBodiesMaxLimit {
		t.Fatalf("limit=9999 clamp: got %d, want <=%d", len(got.Results), searchBodiesMaxLimit)
	}
	// limit=0 / negative also clamps to min, not 400.
	for _, bad := range []string{"0", "-3", "abc"} {
		resp, _ = adminC.Get(base + fmt.Sprintf("?space_id=%d&q=alpha&limit=%s", space, bad))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("limit=%q: status=%d want 200 (silent clamp)", bad, resp.StatusCode)
		}
	}

	// 12. limit=1 returns exactly one result.
	got = getBodySearch(t, adminC, base+fmt.Sprintf("?space_id=%d&q=alpha&limit=1", space))
	if len(got.Results) != 1 {
		t.Fatalf("limit=1: got %d results, want 1", len(got.Results))
	}

	// 13. FTS5 syntax injection: a query containing FTS5 operator chars must
	//     not 500. The sanitiser strips `*`, escapes `"`, and wraps each term
	//     in quotes — the engine sees a well-formed MATCH expression.
	for _, evil := range []string{`+evil-stuff`, `"OR"`, `***`, `(foo OR bar)`} {
		resp, _ = adminC.Get(base + fmt.Sprintf("?space_id=%d&q=%s", space, urlEscape(evil)))
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		// 200 OK is the expected outcome — possibly with empty results. A 500
		// here means the sanitiser let through unbalanced FTS5 syntax.
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("injection q=%q: status=%d body=%s want 200", evil, resp.StatusCode, body)
		}
	}
}

// TestSearchBodies_BearerScopes asserts the M16.A.1 bearer-auth integrations:
// a `read`-scope key works (GET allowed), a `read` key restricted to a
// different space 403s with api_key_space_scope.
//
// Bearer-auth tests require an on-disk DB — LookupAPIKey kicks off an async
// last_used_at goroutine and modernc.org/sqlite's `:memory:` is per-
// connection (see api_keys_test.go's newWiredServerOnDisk for the long-form
// rationale).
func TestSearchBodies_BearerScopes(t *testing.T) {
	t.Setenv("TELA_API_KEY_SECRET", "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb")
	auth.ResetAPIKeySecretCache()
	ts, d := newWiredServerOnDisk(t)

	uid := seedUser(t, d, "admin", "adminpw12", true)
	spaceA := seedSpace(t, d, "Space A", "a", uid)
	spaceB := seedSpace(t, d, "Space B", "b", uid)
	_ = seedPageInSpace(t, d, spaceA, nil, "A page", "alpha widget body")
	_ = seedPageInSpace(t, d, spaceB, nil, "B page", "alpha widget body")

	// Helper: insert a bearer key for `uid` with the given scope + optional
	// space restriction. Returns the raw token.
	seedKey := func(scope string, spaceID *int64) string {
		t.Helper()
		raw, _, _, err := auth.NewAPIKey(auth.LoadAPIKeySecret())
		if err != nil {
			t.Fatalf("new api key: %v", err)
		}
		hmacHex := auth.HMACAPIKey(auth.LoadAPIKeySecret(), raw)
		var spaceArg any = nil
		if spaceID != nil {
			spaceArg = *spaceID
		}
		if _, err := d.ExecContext(context.Background(), `
			INSERT INTO api_keys (user_id, name, key_prefix, key_hmac, scope, space_id)
			VALUES (?, 'k', ?, ?, ?, ?)`,
			uid, raw[:8], hmacHex, scope, spaceArg); err != nil {
			t.Fatalf("seed key: %v", err)
		}
		return raw
	}

	// 1. Unrestricted `read` key → 200 for spaceA.
	readKey := seedKey(auth.ScopeRead, nil)
	r := bearerRequest(t, http.MethodGet,
		ts.URL+fmt.Sprintf("/api/search/bodies?space_id=%d&q=alpha", spaceA),
		readKey, "")
	bodyBytes, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("read key spaceA: status=%d body=%s want 200", r.StatusCode, bodyBytes)
	}

	// 2. Key restricted to spaceA querying spaceB → 403 api_key_space_scope.
	restrictedKey := seedKey(auth.ScopeRead, &spaceA)
	r = bearerRequest(t, http.MethodGet,
		ts.URL+fmt.Sprintf("/api/search/bodies?space_id=%d&q=alpha", spaceB),
		restrictedKey, "")
	bodyBytes, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("restricted key cross-space: status=%d body=%s want 403", r.StatusCode, bodyBytes)
	}
	if !strings.Contains(string(bodyBytes), `"code":"api_key_space_scope"`) {
		t.Fatalf("restricted key cross-space body=%s missing api_key_space_scope", bodyBytes)
	}

	// 3. Restricted key on its allowed space → 200.
	r = bearerRequest(t, http.MethodGet,
		ts.URL+fmt.Sprintf("/api/search/bodies?space_id=%d&q=alpha", spaceA),
		restrictedKey, "")
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("restricted key in-scope: status=%d want 200", r.StatusCode)
	}
}

// TestSearchBodies_LargeSpaceSeed_TopHit seeds 50 pages where one page has
// the highest term-density for the query, then asserts the top-1 result is
// that page. Guards against the score normalisation accidentally inverting
// the ranking direction.
func TestSearchBodies_LargeSpaceSeed_TopHit(t *testing.T) {
	ts, d := newWiredServer(t)
	admin := seedUser(t, d, "admin", "adminpw12", true)
	space := seedSpace(t, d, "Test Space", "test-space", admin)
	adminC := loginClient(t, ts, "admin", "adminpw12")

	// 49 distractor pages with single mentions of "needle".
	for i := 0; i < 49; i++ {
		_ = seedPageInSpace(t, d, space, nil,
			fmt.Sprintf("Distractor %d", i),
			fmt.Sprintf("page %d talks about needle once and lots of other words", i))
	}
	// One target page mentions "needle" many times.
	target := seedPageInSpace(t, d, space, nil, "Target",
		"needle needle needle needle needle needle needle needle")

	got := getBodySearch(t, adminC,
		ts.URL+fmt.Sprintf("/api/search/bodies?space_id=%d&q=needle&limit=5", space))
	if len(got.Results) == 0 {
		t.Fatalf("got 0 results, want >=1")
	}
	if got.Results[0].ID != target {
		t.Fatalf("top hit id=%d, want target id=%d (results=%+v)", got.Results[0].ID, target, got.Results)
	}
	// limit=5 is honoured: at most 5 rows.
	if len(got.Results) > 5 {
		t.Fatalf("got %d results with limit=5", len(got.Results))
	}
}

func getBodySearch(t *testing.T, c *http.Client, url string) searchBodiesResp {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("get %s status=%d body=%s", url, resp.StatusCode, b)
	}
	var got searchBodiesResp
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}
