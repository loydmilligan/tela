package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/zcag/tela/backend/internal/llm"
	"github.com/zcag/tela/backend/internal/rag"
)

func TestRAGDraft_Grounded(t *testing.T) {
	ts, d, srv := newRagServer(t)
	srv.llm = llm.NewServiceWithCompleter(&fakeCompleter{answer: "# Deploying\n\nRun make deploy."})
	alice := seedUser(t, d, "alice", "alicepw12", false)
	sp := seedSpace(t, d, "Alpha", "alpha", alice)
	mustPage(t, d, sp, "Deploy Guide", "## Shipping\nrun make deploy to push the release to production")
	if _, _, err := rag.NewServiceWithEmbedder(d, fakeEmb{}).ReindexSpace(context.Background(), sp); err != nil {
		t.Fatalf("index: %v", err)
	}
	c := loginClient(t, ts, "alice", "alicepw12")
	bodyReq := `{"topic":"how we deploy","space_id":` + strconv.FormatInt(sp, 10) + `}`

	// Grounded draft + sources.
	resp, err := c.Post(ts.URL+"/api/rag/draft", "application/json", strings.NewReader(bodyReq))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("draft = %d body=%q", resp.StatusCode, rb)
	}
	var out struct {
		Draft   string    `json:"draft"`
		Sources []rag.Hit `json:"sources"`
	}
	if err := json.Unmarshal(rb, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Draft, "make deploy") {
		t.Errorf("draft missing canned content: %q", out.Draft)
	}
	if len(out.Sources) == 0 {
		t.Error("draft has no sources")
	}
}

func TestRAGAnswerToPage_CreatesCitedPage(t *testing.T) {
	ts, d, srv := newRagServer(t)
	srv.llm = llm.NewServiceWithCompleter(&fakeCompleter{answer: "You ship with make deploy."})
	alice := seedUser(t, d, "alice", "alicepw12", false)
	sp := seedSpace(t, d, "Alpha", "alpha", alice)
	src := mustPage(t, d, sp, "Deploy Guide", "## Shipping\nrun make deploy to push the release to production")
	if _, _, err := rag.NewServiceWithEmbedder(d, fakeEmb{}).ReindexSpace(context.Background(), sp); err != nil {
		t.Fatalf("index: %v", err)
	}
	c := loginClient(t, ts, "alice", "alicepw12")
	resp, err := c.Post(ts.URL+"/api/rag/answer-to-page", "application/json",
		strings.NewReader(`{"question":"how do I deploy","space_id":`+strconv.FormatInt(sp, 10)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer-to-page = %d body=%q", resp.StatusCode, rb)
	}
	var out struct {
		Page struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"page"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal(rb, &out); err != nil {
		t.Fatal(err)
	}
	if out.Page.ID == 0 {
		t.Fatal("no page created")
	}
	// The created page exists and carries the answer + a Sources section citing the source page.
	var body string
	if err := d.QueryRow(`SELECT body FROM pages WHERE id=$1`, out.Page.ID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "make deploy") || !strings.Contains(body, "## Sources") {
		t.Errorf("saved page body missing answer or sources: %q", body)
	}
	if !strings.Contains(body, "tela://page/"+strconv.FormatInt(src, 10)) {
		t.Errorf("saved page doesn't cite the source page %d: %q", src, body)
	}
}

func TestBuildAskContext_ExpandsHubAndFallsBack(t *testing.T) {
	// 6 pages in rank order. Pages 1–4 expand by rank; page 5 (rank 4, single
	// chunk) must fall back to its chunk; page 6 (LAST by rank but a dense hub —
	// the "kafka registry" shape) must still expand via the density rule.
	pageIDs := []int64{1, 2, 3, 4, 5, 6}
	best := map[int64]rag.Hit{}
	bodies := map[int64]string{}
	contents := map[int64]string{}
	count := map[int64]int{}
	for _, pid := range pageIDs {
		best[pid] = rag.Hit{PageID: pid, ChunkID: pid * 10, Title: "Page" + strconv.FormatInt(pid, 10),
			HeadingPath: "Sec" + strconv.FormatInt(pid, 10), Snippet: "snip"}
		bodies[pid] = "FULLBODY" + strconv.FormatInt(pid, 10) + " whole page text"
		contents[pid*10] = "CHUNK" + strconv.FormatInt(pid, 10) + " fragment"
		count[pid] = 1
	}
	count[6] = askDenseChunks // page 6 is the dense hub despite ranking last

	block, pageHits := buildAskContext(pageIDs, best, count, bodies, contents)

	// Top-ranked pages expanded to full body.
	if !strings.Contains(block, "FULLBODY1") {
		t.Errorf("top page not expanded: %q", block)
	}
	// Page 5: not top-rank, not dense → chunk fallback, with heading path in header.
	if strings.Contains(block, "FULLBODY5") || !strings.Contains(block, "CHUNK5") {
		t.Errorf("page 5 should fall back to its chunk, got: %q", block)
	}
	if !strings.Contains(block, "Page5 — Sec5") {
		t.Errorf("chunk-fallback header should carry heading path: %q", block)
	}
	// Page 6: the density rescue — expanded to full body even though it ranks last.
	if !strings.Contains(block, "FULLBODY6") {
		t.Errorf("dense hub page (rank last) was not expanded — the table-rescue case: %q", block)
	}
	// Per-page hits align with the [n] numbering (one per page, in order).
	if len(pageHits) != 6 {
		t.Fatalf("want 6 page hits, got %d", len(pageHits))
	}
	for i, h := range pageHits {
		if h.PageID != pageIDs[i] {
			t.Errorf("pageHits[%d] = page %d, want %d", i, h.PageID, pageIDs[i])
		}
	}
}

func TestLowConfidence(t *testing.T) {
	cases := []struct {
		name     string
		rerankOn bool
		top      float64
		want     bool
	}{
		{"strong query, rerank on", true, 3.3, false},
		{"answerable aggregate, rerank on", true, -0.2, false},
		{"out-of-scope, rerank on", true, -6.6, true},
		{"just over threshold", true, -4.1, true},
		{"just under threshold", true, -3.9, false},
		{"rerank off never flags (RRF scale differs)", false, -6.6, false},
	}
	for _, c := range cases {
		if got := lowConfidence(c.rerankOn, c.top); got != c.want {
			t.Errorf("%s: lowConfidence(%v, %.1f) = %v, want %v", c.name, c.rerankOn, c.top, got, c.want)
		}
	}
}

func TestRAGAsk_Followups(t *testing.T) {
	ts, d, srv := newRagServer(t)
	srv.llm = llm.NewServiceWithCompleter(&fakeCompleter{answer: "Use make deploy to ship."})
	alice := seedUser(t, d, "alice", "alicepw12", false)
	sp := seedSpace(t, d, "Alpha", "alpha", alice)
	mustPage(t, d, sp, "Deploy Guide", "## Shipping\nrun make deploy to push the release to production")
	if _, _, err := rag.NewServiceWithEmbedder(d, fakeEmb{}).ReindexSpace(context.Background(), sp); err != nil {
		t.Fatalf("index: %v", err)
	}
	c := loginClient(t, ts, "alice", "alicepw12")
	resp, err := c.Post(ts.URL+"/api/rag/ask", "application/json",
		strings.NewReader(`{"question":"how do I deploy","space_id":`+strconv.FormatInt(sp, 10)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	var out struct {
		Answer    string   `json:"answer"`
		Followups []string `json:"followups"`
	}
	if err := json.Unmarshal(rb, &out); err != nil {
		t.Fatal(err)
	}
	if out.Answer == "" {
		t.Fatal("no answer")
	}
	if len(out.Followups) == 0 {
		t.Error("expected follow-up questions")
	}
}
