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
	EmbedURL   string // Ollama base, e.g. http://ollama-host:11435, or tela cloud's /api/cloud/ollama
	EmbedModel string // default qwen3-embedding:0.6b (1024-d)
	EmbedToken string // optional bearer (a tela PAT) when EmbedURL is the managed cloud endpoint
	Dim        int    // expected embedding dimension (advisory; column is vector(1024))

	// Reranking (optional second-stage precision). Empty RerankURL => disabled
	// (hybrid+RRF order is returned as-is). When set, the top fused candidates are
	// re-scored by a cross-encoder before the final trim.
	RerankURL   string // full /rerank endpoint (Cohere/Jina-compatible shape)
	RerankModel string
	RerankToken string
}

// ConfigFromEnv reads the TELA_RAG_* env. The default embed model tracks the prod
// instance (qwen3-embedding:0.6b, 1024-d). Reranking is off unless
// TELA_RAG_RERANK_URL is set.
func ConfigFromEnv() Config {
	return Config{
		EmbedURL:    os.Getenv("TELA_RAG_EMBED_URL"),
		EmbedModel:  getenv("TELA_RAG_EMBED_MODEL", "qwen3-embedding:0.6b"),
		EmbedToken:  os.Getenv("TELA_RAG_EMBED_TOKEN"),
		Dim:         atoiDefault(os.Getenv("TELA_RAG_EMBED_DIM"), 1024),
		RerankURL:   os.Getenv("TELA_RAG_RERANK_URL"),
		RerankModel: os.Getenv("TELA_RAG_RERANK_MODEL"),
		RerankToken: os.Getenv("TELA_RAG_RERANK_TOKEN"),
	}
}

// Embedder turns text into a vector. One method (plus Model for cache-keying),
// so swapping Ollama for a hosted OpenAI-compatible endpoint later is a new
// file implementing this interface, not a refactor.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Model() string
}

// Service bundles the DB handle, config, the active embedder, and an optional
// reranker. A nil embedder means the feature is disabled; a nil reranker means
// the fused order is returned as-is.
type Service struct {
	db  *sql.DB
	cfg Config
	emb Embedder
	rr  Reranker

	// Auto-reindex queue (see autoreindex.go). pending maps page id → debounce
	// deadline; nil until StartAutoReindex runs. attempts tracks consecutive
	// failures per page to drive exponential retry backoff (cleared on success).
	// pendingFiles/fileAttempts are the identical machinery for space_file ids
	// (the file half of the document index). All guarded by queueMu.
	queueMu      sync.Mutex
	pending      map[int64]time.Time
	attempts     map[int64]int
	pendingFiles map[int64]time.Time
	fileAttempts map[int64]int

	// paused, when set and true, halts background semantic backfilling (the
	// auto-reindex worker + stale sweep). Wired to the admin AI kill-switch so
	// indexing doesn't hammer the embedder while it's under maintenance; the
	// corpus is the source of truth, so the sweep backfills once it clears.
	paused func() bool
}

// SetPaused installs the predicate the background indexer consults each tick.
// Call before StartAutoReindex.
func (s *Service) SetPaused(fn func() bool) { s.paused = fn }

func (s *Service) isPaused() bool { return s.paused != nil && s.paused() }

// NewService builds the service from config. It never fails: with no EmbedURL
// the service is constructed disabled so api.Server can hold a non-nil handle
// unconditionally.
func NewService(db *sql.DB, cfg Config) *Service {
	s := &Service{db: db, cfg: cfg}
	if cfg.EmbedURL != "" {
		s.emb = NewOllamaEmbedder(cfg.EmbedURL, cfg.EmbedModel, cfg.EmbedToken)
	}
	if cfg.RerankURL != "" {
		s.rr = NewHTTPReranker(cfg.RerankURL, cfg.RerankModel, cfg.RerankToken)
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
