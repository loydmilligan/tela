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

// fakeStreamCompleter implements BOTH Completer and llm.StreamCompleter, emitting
// its tokens one at a time — so the stream endpoint exercises the real per-token
// path (not the blocking fallback the bare fakeCompleter hits).
type fakeStreamCompleter struct{ tokens []string }

func (f *fakeStreamCompleter) Model() string { return "fake-stream" }
func (f *fakeStreamCompleter) Complete(_ context.Context, _, _ string) (string, error) {
	return strings.Join(f.tokens, ""), nil
}
func (f *fakeStreamCompleter) CompleteStream(_ context.Context, _, _ string, onToken func(string) error) error {
	for _, t := range f.tokens {
		if err := onToken(t); err != nil {
			return err
		}
	}
	return nil
}

type sseEvent struct{ name, data string }

// parseSSE collects the (event, data) frames from a buffered SSE body. Test-only:
// the handler writes every frame then returns, so reading the whole body is fine.
func parseSSE(body []byte) []sseEvent {
	var evs []sseEvent
	for _, block := range strings.Split(strings.TrimSpace(string(body)), "\n\n") {
		var ev sseEvent
		for _, line := range strings.Split(block, "\n") {
			if d, ok := strings.CutPrefix(line, "event: "); ok {
				ev.name = d
			}
			if d, ok := strings.CutPrefix(line, "data: "); ok {
				ev.data = d
			}
		}
		if ev.name != "" {
			evs = append(evs, ev)
		}
	}
	return evs
}

func streamTokens(evs []sseEvent) string {
	var b strings.Builder
	for _, e := range evs {
		if e.name != "token" {
			continue
		}
		var tk struct {
			T string `json:"t"`
		}
		_ = json.Unmarshal([]byte(e.data), &tk)
		b.WriteString(tk.T)
	}
	return b.String()
}

func hasEvent(evs []sseEvent, name string) bool {
	for _, e := range evs {
		if e.name == name {
			return true
		}
	}
	return false
}

// A grounded ask streams: sources first, then the answer tokens, then done.
func TestRAGAskStream_StreamsTokensAndSources(t *testing.T) {
	ts, d, srv := newRagServer(t)
	srv.llm = llm.NewServiceWithCompleter(&fakeStreamCompleter{tokens: []string{"Run ", "make ", "deploy."}})

	alice := seedUser(t, d, "alice", "alicepw12", false)
	aSpace := seedSpace(t, d, "Alpha", "alpha", alice)
	page := mustPage(t, d, aSpace, "Deploy Guide", "## Shipping\nrun make deploy to push the release to production")
	if _, _, err := rag.NewServiceWithEmbedder(d, fakeEmb{}).ReindexSpace(context.Background(), aSpace); err != nil {
		t.Fatalf("index: %v", err)
	}

	c := loginClient(t, ts, "alice", "alicepw12")
	resp, err := c.Post(ts.URL+"/api/rag/ask/stream", "application/json",
		strings.NewReader(`{"question":"how do I deploy a release","space_id":`+strconv.FormatInt(aSpace, 10)+`}`))
	if err != nil {
		t.Fatalf("ask stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%q want 200", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type=%q want text/event-stream", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	evs := parseSSE(body)

	if !hasEvent(evs, "sources") || !hasEvent(evs, "token") || !hasEvent(evs, "done") {
		t.Fatalf("missing expected events in %s", body)
	}
	// sources event names the cited page, before any token.
	if evs[0].name != "sources" {
		t.Fatalf("first event=%q want sources", evs[0].name)
	}
	if !strings.Contains(evs[0].data, `"page_id":`+strconv.FormatInt(page, 10)) {
		t.Fatalf("sources event missing page %d: %s", page, evs[0].data)
	}
	// Three distinct token frames reassemble into the answer.
	var tokenFrames int
	for _, e := range evs {
		if e.name == "token" {
			tokenFrames++
		}
	}
	if tokenFrames != 3 {
		t.Fatalf("token frames=%d want 3 (true per-token streaming): %s", tokenFrames, body)
	}
	if got := streamTokens(evs); got != "Run make deploy." {
		t.Fatalf("reassembled answer=%q want %q", got, "Run make deploy.")
	}
}

// A client whose LLM only implements Complete still streams — via the blocking
// fallback that delivers the whole answer as one token frame.
func TestRAGAskStream_FallbackForNonStreamingClient(t *testing.T) {
	ts, d, srv := newRagServer(t)
	srv.llm = llm.NewServiceWithCompleter(&fakeCompleter{answer: "Run make deploy to ship."})

	alice := seedUser(t, d, "alice", "alicepw12", false)
	aSpace := seedSpace(t, d, "Alpha", "alpha", alice)
	_ = mustPage(t, d, aSpace, "Deploy Guide", "## Shipping\nrun make deploy to push the release to production")
	if _, _, err := rag.NewServiceWithEmbedder(d, fakeEmb{}).ReindexSpace(context.Background(), aSpace); err != nil {
		t.Fatalf("index: %v", err)
	}

	c := loginClient(t, ts, "alice", "alicepw12")
	resp, err := c.Post(ts.URL+"/api/rag/ask/stream", "application/json",
		strings.NewReader(`{"question":"how do I deploy","space_id":`+strconv.FormatInt(aSpace, 10)+`}`))
	if err != nil {
		t.Fatalf("ask stream: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	evs := parseSSE(body)
	if !hasEvent(evs, "done") {
		t.Fatalf("no done event: %s", body)
	}
	if got := streamTokens(evs); !strings.Contains(got, "make deploy") {
		t.Fatalf("fallback answer missing text: %q", got)
	}
}

// With the LLM unconfigured the stream endpoint 503s cleanly — a real HTTP
// status, NOT a mid-stream error frame (the guard runs before any SSE byte).
func TestRAGAskStream_DisabledReturns503(t *testing.T) {
	ts, d, _ := newRagServer(t) // rag enabled, llm unconfigured
	_ = seedUser(t, d, "alice", "alicepw12", false)
	c := loginClient(t, ts, "alice", "alicepw12")
	resp, err := c.Post(ts.URL+"/api/rag/ask/stream", "application/json", strings.NewReader(`{"question":"hi"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503 when llm unconfigured", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("503 should not be an SSE stream, got content-type %q", ct)
	}
}
