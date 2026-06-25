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
	"strconv"
	"strings"
)

// Config is the env-driven configuration. URL empty => feature disabled.
type Config struct {
	URL       string // OpenAI-compatible base, e.g. http://ollama-host:11434/v1, or tela cloud's /api/cloud/llm/v1
	Model     string // chat model, e.g. qwen2.5:7b
	Token     string // optional bearer (a tela PAT) when URL is the managed cloud endpoint
	MaxTokens int    // completion length cap (0 => provider default)
}

// ConfigFromEnv reads TELA_LLM_URL / _MODEL / _TOKEN / _MAX_TOKENS. _TOKEN is set
// only when pointing URL at tela cloud's managed LLM endpoint. _MAX_TOKENS caps
// answer length (default 1024) so a slow local model can't generate for minutes
// and trip the request timeout; set 0 to disable the cap.
func ConfigFromEnv() Config {
	return Config{
		URL:       os.Getenv("TELA_LLM_URL"),
		Model:     getenv("TELA_LLM_MODEL", "qwen2.5:7b"),
		Token:     os.Getenv("TELA_LLM_TOKEN"),
		MaxTokens: atoiDefault(os.Getenv("TELA_LLM_MAX_TOKENS"), 1024),
	}
}

// Completer turns a system+user prompt into a single completion. One method, so
// the test fake and a future hosted provider are sibling types implementing this
// interface — not a refactor.
type Completer interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	Model() string
}

// StreamCompleter is the optional streaming counterpart to Completer. A client
// that implements it (the OpenAI client) streams token deltas; one that doesn't
// (test fakes, a future provider) is handled by Service.CompleteStream's
// blocking fallback, so callers can always stream without every client growing
// a streaming path.
type StreamCompleter interface {
	CompleteStream(ctx context.Context, systemPrompt, userPrompt string, onToken func(string) error) error
}

// UsageRecorder is called once per completion with the active model and
// length-based token estimates. Injected by the api layer (which owns the DB);
// nil = no metering. See api/ai_usage.go.
type UsageRecorder func(model string, inputTokens, outputTokens int)

// EstimateTokens is the shared rough heuristic (~4 chars/token) used to meter AI
// usage without a real tokenizer — adequate for cost estimation, not billing.
func EstimateTokens(s string) int { return (len(s) + 3) / 4 }

// Service bundles the config and the active client. A nil client means disabled.
type Service struct {
	cfg Config
	cl  Completer
	// usage, when set, records token estimates for every completion — the single
	// chokepoint for all chat usage regardless of which package calls Complete.
	usage UsageRecorder
}

// SetUsageRecorder installs the per-completion usage hook. Call once at wiring.
func (s *Service) SetUsageRecorder(r UsageRecorder) {
	if s != nil {
		s.usage = r
	}
}

func (s *Service) record(systemPrompt, userPrompt, output string) {
	if s.usage == nil {
		return
	}
	s.usage(s.Model(), EstimateTokens(systemPrompt)+EstimateTokens(userPrompt), EstimateTokens(output))
}

// NewService builds the service from config. It never fails: with no URL the
// service is constructed disabled so api.Server can hold a non-nil handle
// unconditionally.
func NewService(cfg Config) *Service {
	s := &Service{cfg: cfg}
	if cfg.URL != "" {
		s.cl = NewOpenAIClient(cfg.URL, cfg.Model, cfg.Token, cfg.MaxTokens)
	}
	return s
}

// NewServiceWithCompleter injects a completer directly — used by tests with a
// canned fake and by the managed proxy, bypassing the env/HTTP path.
func NewServiceWithCompleter(cl Completer) *Service { return &Service{cl: cl} }

// Enabled reports whether a chat client is configured.
func (s *Service) Enabled() bool { return s != nil && s.cl != nil }

// liveChecker is the optional liveness probe a chat client can expose so the
// background AI-health prober can confirm reachability without a completion. A
// client without one (a test fake) is treated as reachable.
type liveChecker interface {
	Live(ctx context.Context) error
}

// Ping checks the chat backend is reachable, for the AI-health prober. Defers to
// the client's Live check; a client without one is treated as reachable. A
// disabled service returns errLLMDisabled.
func (s *Service) Ping(ctx context.Context) error {
	if !s.Enabled() {
		return errLLMDisabled
	}
	if lc, ok := s.cl.(liveChecker); ok {
		return lc.Live(ctx)
	}
	return nil
}

var errLLMDisabled = errors.New("llm: client disabled")

// Complete runs one non-streaming chat completion. Surfaces the disabled error
// so callers (and the cloud proxy) can 503 uniformly.
func (s *Service) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if !s.Enabled() {
		return "", errLLMDisabled
	}
	out, err := s.cl.Complete(ctx, systemPrompt, userPrompt)
	if err == nil {
		s.record(systemPrompt, userPrompt, out)
	}
	return out, err
}

// CompleteStream streams the completion token-by-token via onToken. If the active
// client doesn't implement StreamCompleter, it falls back to a blocking Complete
// and delivers the whole answer as a single onToken call — so the streaming ask
// path works against any provider (and the test fakes) unchanged.
func (s *Service) CompleteStream(ctx context.Context, systemPrompt, userPrompt string, onToken func(string) error) error {
	if !s.Enabled() {
		return errLLMDisabled
	}
	// Count streamed output for the usage estimate without buffering the whole
	// answer: tally bytes as they flow, record on success.
	var outLen int
	tally := func(tok string) error {
		outLen += len(tok)
		return onToken(tok)
	}
	if sc, ok := s.cl.(StreamCompleter); ok {
		err := sc.CompleteStream(ctx, systemPrompt, userPrompt, tally)
		if err == nil && s.usage != nil {
			s.usage(s.Model(), EstimateTokens(systemPrompt)+EstimateTokens(userPrompt), (outLen+3)/4)
		}
		return err
	}
	out, err := s.cl.Complete(ctx, systemPrompt, userPrompt)
	if err != nil {
		return err
	}
	s.record(systemPrompt, userPrompt, out)
	if out == "" {
		return nil
	}
	return onToken(out)
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

// atoiDefault parses s as an int, falling back to def on empty/invalid. A
// negative value (e.g. "-1") is treated as "no cap" → 0.
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	if n < 0 {
		return 0
	}
	return n
}
