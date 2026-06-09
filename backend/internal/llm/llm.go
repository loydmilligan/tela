// Package llm is tela's env-gated chat-completion client — the sibling to
// internal/rag for text generation. It speaks the OpenAI-compatible
// /v1/chat/completions shape (which Ollama also serves), so one client covers
// both a BYO endpoint and tela cloud's managed proxy.
//
// Wire-in mirrors rag exactly: one field on api.Server (s.llm), constructed from
// env. When TELA_LLM_URL is unset the service is disabled (Enabled()==false;
// handlers 503), so it ships dark until explicitly configured. The single client
// is consumed in-process by /api/rag/ask AND over HTTP by self-hosters via the
// managed cloud proxy (api.CloudChat) — there is no second generation path.
package llm

import (
	"context"
	"errors"
	"os"
)

// Config is the env-driven configuration. URL empty => feature disabled.
type Config struct {
	URL   string // OpenAI-compatible base, e.g. http://ollama-host:11434/v1, or tela cloud's /api/cloud/llm/v1
	Model string // chat model, e.g. qwen2.5:7b
	Token string // optional bearer (a tela PAT) when URL is the managed cloud endpoint
}

// ConfigFromEnv reads TELA_LLM_URL / _MODEL / _TOKEN. _TOKEN is set only when
// pointing URL at tela cloud's managed LLM endpoint.
func ConfigFromEnv() Config {
	return Config{
		URL:   os.Getenv("TELA_LLM_URL"),
		Model: getenv("TELA_LLM_MODEL", "qwen2.5:7b"),
		Token: os.Getenv("TELA_LLM_TOKEN"),
	}
}

// Completer turns a system+user prompt into a single completion. One method, so
// the test fake and a future hosted provider are sibling types implementing this
// interface — not a refactor.
type Completer interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	Model() string
}

// Service bundles the config and the active client. A nil client means disabled.
type Service struct {
	cfg Config
	cl  Completer
}

// NewService builds the service from config. It never fails: with no URL the
// service is constructed disabled so api.Server can hold a non-nil handle
// unconditionally.
func NewService(cfg Config) *Service {
	s := &Service{cfg: cfg}
	if cfg.URL != "" {
		s.cl = NewOpenAIClient(cfg.URL, cfg.Model, cfg.Token)
	}
	return s
}

// NewServiceWithCompleter injects a completer directly — used by tests with a
// canned fake and by the managed proxy, bypassing the env/HTTP path.
func NewServiceWithCompleter(cl Completer) *Service { return &Service{cl: cl} }

// Enabled reports whether a chat client is configured.
func (s *Service) Enabled() bool { return s != nil && s.cl != nil }

var errLLMDisabled = errors.New("llm: client disabled")

// Complete runs one non-streaming chat completion. Surfaces the disabled error
// so callers (and the cloud proxy) can 503 uniformly.
func (s *Service) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if !s.Enabled() {
		return "", errLLMDisabled
	}
	return s.cl.Complete(ctx, systemPrompt, userPrompt)
}

// Model returns the active model name (for the managed proxy default).
func (s *Service) Model() string {
	if s.cl == nil {
		return ""
	}
	return s.cl.Model()
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
