package rag

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOpenAIEmbedder_Embed proves the OpenAI path posts to /embeddings, parses
// the {"data":[{"embedding"}]} shape, and routes a query through the asymmetric
// instruction prefix (the parity check vs the Ollama embedder, so a LiteLLM
// proxy is a drop-in swap).
func TestOpenAIEmbedder_Embed(t *testing.T) {
	var gotPath, gotInput string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Input string `json:"input"`
		}
		_ = json.Unmarshal(body, &req)
		gotInput = req.Input
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float32{0.1, 0.2, 0.3}}},
		})
	}))
	defer srv.Close()

	emb := NewOpenAIEmbedder(srv.URL+"/v1", "text-embedding-3-small", "")
	vec, err := emb.Embed(context.Background(), "a passage")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotPath != "/v1/embeddings" {
		t.Fatalf("expected POST /v1/embeddings, got %q", gotPath)
	}
	if len(vec) != 3 || vec[0] != 0.1 {
		t.Fatalf("unexpected vector %v", vec)
	}
	if gotInput != "a passage" {
		t.Fatalf("passage should embed bare, got %q", gotInput)
	}

	if _, err := emb.EmbedQuery(context.Background(), "how do we deploy"); err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if !strings.HasPrefix(gotInput, "Instruct: ") || !strings.Contains(gotInput, "\nQuery:how do we deploy") {
		t.Fatalf("query not instruction-wrapped: %q", gotInput)
	}
}

// TestOpenAIEmbedder_OverflowShrinks proves an over-long input that the upstream
// rejects with a 400 "context length" is shrunk and retried (the shared
// shrinkToFit loop), eventually succeeding — never an infinite loop.
func TestOpenAIEmbedder_OverflowShrinks(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Input string `json:"input"`
		}
		_ = json.Unmarshal(body, &req)
		if len([]rune(req.Input)) > 200 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"This model's maximum context length is 512 tokens"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float32{0.5}}},
		})
	}))
	defer srv.Close()

	emb := NewOpenAIEmbedder(srv.URL+"/v1", "m", "")
	vec, err := emb.Embed(context.Background(), strings.Repeat("x", 1600))
	if err != nil {
		t.Fatalf("Embed should succeed after shrinking: %v", err)
	}
	if len(vec) != 1 {
		t.Fatalf("unexpected vector %v", vec)
	}
	if calls < 2 {
		t.Fatalf("expected at least one shrink retry, got %d call(s)", calls)
	}
}

// TestIsOpenAIBase pins the embedder-selection rule: a /v1 base => OpenAI shape,
// a bare Ollama host => native /api/embed.
func TestIsOpenAIBase(t *testing.T) {
	cases := map[string]bool{
		"http://litellm:4000/v1":  true,
		"http://litellm:4000/v1/": true,
		"http://ollama:11434":     false,
		"https://x/api/cloud/ollama": false,
	}
	for url, want := range cases {
		if got := isOpenAIBase(url); got != want {
			t.Errorf("isOpenAIBase(%q) = %v, want %v", url, got, want)
		}
	}
}
