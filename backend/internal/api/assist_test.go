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
