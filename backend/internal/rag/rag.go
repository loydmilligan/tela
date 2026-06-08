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
	"errors"
	"os"
	"strconv"
	"sync"
	"time"
)

// Config is the env-driven configuration. EmbedURL empty => feature disabled.
type Config struct {
	EmbedURL   string // Ollama base, e.g. http://tardis:11435, or tela cloud's /api/cloud/ollama
	EmbedModel string // default qwen3-embedding:0.6b (1024-d)
	EmbedToken string // optional bearer (a tela PAT) when EmbedURL is the managed cloud endpoint
	Dim        int    // expected embedding dimension (advisory; column is vector(1024))
}

// ConfigFromEnv reads TELA_RAG_EMBED_URL / _MODEL / _TOKEN / _DIM. The default
// model tracks the prod embed instance (qwen3-embedding:0.6b, 1024-d); override
// per deployment with TELA_RAG_EMBED_MODEL. _TOKEN is set only when pointing at
// tela cloud's managed embed endpoint.
func ConfigFromEnv() Config {
	return Config{
		EmbedURL:   os.Getenv("TELA_RAG_EMBED_URL"),
		EmbedModel: getenv("TELA_RAG_EMBED_MODEL", "qwen3-embedding:0.6b"),
		EmbedToken: os.Getenv("TELA_RAG_EMBED_TOKEN"),
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

	// Auto-reindex queue (see autoreindex.go). pending maps page id → debounce
	// deadline; nil until StartAutoReindex runs. Guarded by queueMu.
	queueMu sync.Mutex
	pending map[int64]time.Time
}

// NewService builds the service from config. It never fails: with no EmbedURL
// the service is constructed disabled so api.Server can hold a non-nil handle
// unconditionally.
func NewService(db *sql.DB, cfg Config) *Service {
	s := &Service{db: db, cfg: cfg}
	if cfg.EmbedURL != "" {
		s.emb = NewOllamaEmbedder(cfg.EmbedURL, cfg.EmbedModel, cfg.EmbedToken)
	}
	return s
}

// Embed exposes the active embedder for the managed cloud embed proxy
// (api.CloudEmbed), so a connected self-hoster's embeddings are produced by the
// SAME embedder the provider uses in-process — no second embedding path.
func (s *Service) Embed(ctx context.Context, text string) ([]float32, error) {
	if !s.Enabled() {
		return nil, errEmbedderDisabled
	}
	return s.emb.Embed(ctx, text)
}

// EmbedModel returns the active model name (for the managed proxy response).
func (s *Service) EmbedModel() string {
	if s.emb == nil {
		return ""
	}
	return s.emb.Model()
}

var errEmbedderDisabled = errors.New("rag: embedder disabled")

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
