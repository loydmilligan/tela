package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// getSearch runs GET /api/search?q= as the given client and returns the hits.
func getSearch(t *testing.T, c *http.Client, base, q string) []searchHit {
	t.Helper()
	resp, err := c.Get(base + "/api/search?q=" + url.QueryEscape(q))
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search %q: status %d", q, resp.StatusCode)
	}
	var out struct {
		Results []searchHit `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out.Results
}

func TestSearch_RanksTitleOverBodyAndScopes(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	bob := seedUser(t, d, "bob", "bobpw1234", false)
	aSpace := seedSpace(t, d, "Alpha", "alpha", alice)
	bSpace := seedSpace(t, d, "Bravo", "bravo", bob) // alice not a member

	titleHit := mustPage(t, d, aSpace, "Deploy", "general notes about nothing in particular")
	bodyHit := mustPage(t, d, aSpace, "Onboarding", "you deploy the release by running make deploy in prod")
	secret := mustPage(t, d, bSpace, "Deploy Secrets", "deploy deploy deploy roadmap")

	c := loginClient(t, ts, "alice", "alicepw12")
	hits := getSearch(t, c, ts.URL, "deploy")

	if len(hits) < 2 {
		t.Fatalf("want >=2 hits, got %d", len(hits))
	}
	// Title match (weight A) outranks body-only match (weight B).
	if hits[0].PageID != titleHit {
		t.Errorf("top hit = %d, want title match %d", hits[0].PageID, titleHit)
	}
	// Body match present, and its snippet is <mark>-highlighted.
	var body *searchHit
	for i := range hits {
		if hits[i].PageID == bodyHit {
			body = &hits[i]
		}
		if hits[i].PageID == secret {
			t.Fatalf("LEAK: alice got bob's page %d", secret)
		}
	}
	if body == nil {
		t.Fatal("body-match page missing from results")
	}
	if !strings.Contains(body.Snippet, "<mark>") {
		t.Errorf("body snippet not highlighted: %q", body.Snippet)
	}
}

func TestSearch_TolerantOfPunctuation(t *testing.T) {
	ts, d := newWiredServer(t)
	alice := seedUser(t, d, "alice", "alicepw12", false)
	sp := seedSpace(t, d, "Alpha", "alpha", alice)
	mustPage(t, d, sp, "C++ Notes", "operator overloading in c++")

	c := loginClient(t, ts, "alice", "alicepw12")
	// Raw punctuation/operators must never 500 (websearch_to_tsquery is forgiving).
	for _, q := range []string{"c++", `"unterminated`, "a & b |", "!!!", "deploy!"} {
		resp, err := c.Get(ts.URL + "/api/search?q=" + url.QueryEscape(q))
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("search %q: status %d, want 200", q, resp.StatusCode)
		}
	}
}
