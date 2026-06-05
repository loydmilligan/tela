// Package rag is tela's self-contained semantic-retrieval layer: heading-aware
// markdown chunking, pluggable embeddings (Ollama by default), and hybrid
// (lexical + vector, RRF-fused) chunk search over the page_chunks table.
//
// Storage is Postgres + pgvector (migrations 0002/0003). Two sticky invariants
// shape every query here:
//
//   - The index is a DISPOSABLE, derived cache of `pages`. page_chunks is fully
//     rebuildable via ReindexPage/ReindexSpace; backend choices stay reversible.
//   - Authorize through the LIVE page row, in-query. Every retrieval joins
//     page_chunks → pages → space_access; the chunk carries NO permission copy
//     (there is deliberately no space_id column on page_chunks). This is the
//     anti-leak invariant — a chunk can never out-live or out-scope its page.
//
// Wire-in is one field on api.Server plus the routes in api/rag.go. When
// TELA_RAG_EMBED_URL is unset the feature no-ops (Enabled()==false; handlers
// 503), so it ships dark until explicitly configured.
package rag

import (
	"context"
	"database/sql"
	"os"
	"strconv"
)

// Config is the env-driven configuration. EmbedURL empty => feature disabled.
type Config struct {
	EmbedURL   string // Ollama base, e.g. http://tardis:11434
	EmbedModel string // default mxbai-embed-large (1024-d)
	Dim        int    // expected embedding dimension (advisory; column is vector(1024))
}

// ConfigFromEnv reads TELA_RAG_EMBED_URL / _MODEL / _DIM.
func ConfigFromEnv() Config {
	return Config{
		EmbedURL:   os.Getenv("TELA_RAG_EMBED_URL"),
		EmbedModel: getenv("TELA_RAG_EMBED_MODEL", "mxbai-embed-large"),
		Dim:        atoiDefault(os.Getenv("TELA_RAG_EMBED_DIM"), 1024),
	}
}

// Embedder turns text into a vector. One method (plus Model for cache-keying),
// so swapping Ollama for a hosted OpenAI-compatible endpoint later is a new
// file implementing this interface, not a refactor.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Model() string
}

// Service bundles the DB handle, config, and the active embedder. A nil
// embedder means the feature is disabled.
type Service struct {
	db  *sql.DB
	cfg Config
	emb Embedder
}

// NewService builds the service from config. It never fails: with no EmbedURL
// the service is constructed disabled so api.Server can hold a non-nil handle
// unconditionally.
func NewService(db *sql.DB, cfg Config) *Service {
	s := &Service{db: db, cfg: cfg}
	if cfg.EmbedURL != "" {
		s.emb = NewOllamaEmbedder(cfg.EmbedURL, cfg.EmbedModel)
	}
	return s
}

// NewServiceWithEmbedder injects an embedder directly — used by tests with a
// deterministic fake, bypassing the env/Ollama path.
func NewServiceWithEmbedder(db *sql.DB, emb Embedder) *Service {
	return &Service{db: db, emb: emb}
}

// Enabled reports whether an embedder is configured.
func (s *Service) Enabled() bool { return s != nil && s.emb != nil }

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}
