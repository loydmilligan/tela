package rag

import (
	"context"
	"os"
	"testing"

	"github.com/zcag/tela/backend/internal/testdb"
)

// TestSmoke_LiveSemantic exercises the real Ollama embedder end-to-end: it
// proves vectors capture MEANING, not just keywords, by retrieving a chunk with
// a paraphrase query that shares almost no words with the source text. Skipped
// unless TELA_RAG_EMBED_URL is set (e.g. http://ollama-host:11434).
//
//	TELA_RAG_EMBED_URL=http://ollama-host:11434 go test ./internal/rag -run Smoke -v
func TestSmoke_LiveSemantic(t *testing.T) {
	url := os.Getenv("TELA_RAG_EMBED_URL")
	if url == "" {
		t.Skip("set TELA_RAG_EMBED_URL to run the live embedder smoke test")
	}
	model := os.Getenv("TELA_RAG_EMBED_MODEL")
	if model == "" {
		model = "mxbai-embed-large"
	}

	d := testdb.New(t)
	ctx := context.Background()
	u := newUser(t, d, "smoke")
	sp := newSpace(t, d, "smoke-space", u)

	// Source text deliberately avoids the query's vocabulary.
	relevant := newPage(t, d, sp, "Shipping changes",
		"## How it works\nRun make deploy to push the latest build out to the live servers; a health gate verifies it before traffic flows.")
	newPage(t, d, sp, "Banana bread",
		"## Recipe\nMash ripe bananas, fold in flour and sugar, bake at 180C for an hour until golden.")

	svc := NewServiceWithEmbedder(d, NewOllamaEmbedder(url, model, ""))
	if _, _, err := svc.ReindexSpace(ctx, sp); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	// Paraphrase: no shared content words with the source ("deploy", "build",
	// "servers" vs "release", "production", "customers").
	hits, err := svc.Search(ctx, u, "how do we release a new version to production for customers", nil, 5, "semantic")
	if err != nil {
		t.Fatalf("semantic search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits from live semantic search")
	}
	if hits[0].PageID != relevant {
		t.Fatalf("top semantic hit = page %d (%q), want the shipping page %d",
			hits[0].PageID, hits[0].Title, relevant)
	}
	t.Logf("✓ live semantic top hit: %q (score %.4f) — %q", hits[0].Title, hits[0].Score, hits[0].Snippet)
}
