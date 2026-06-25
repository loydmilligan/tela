package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIClient calls an OpenAI-compatible POST {base}/chat/completions endpoint.
// "BYO" in tela's case is an Ollama on the tailnet (its /v1 surface) or any
// OpenAI-compatible provider; the managed endpoint is tela cloud's
// /api/cloud/llm/v1, which speaks the same shape — so this one client serves
// both BYO and cloud-backed, no separate cloud completer type.
type OpenAIClient struct {
	base      string // includes the /v1 (or equivalent) prefix; we append /chat/completions
	model     string
	token     string // optional bearer; set when base points at tela's managed endpoint
	maxTokens int    // completion length cap; 0 => unbounded (provider default)
	client    *http.Client
	// stall is the streaming inactivity timeout: a stream is aborted only after
	// this long with NO bytes (not deltas — the upstream's keepalive comments count
	// too), so a long-but-healthy generation never trips it. A field, not a const,
	// so tests can shrink it. See CompleteStream.
	stall time.Duration
}

const (
	// completeTotalTimeout bounds a BLOCKING (non-streaming) completion. The whole
	// exchange must finish within it — correct for Complete, where there's no
	// stream of bytes to prove liveness.
	completeTotalTimeout = 120 * time.Second
	// streamStallTimeout bounds INACTIVITY on a streaming completion: the gap
	// between bytes, reset on each read. Generous because the slow part (prompt
	// processing on a large local model) still emits keepalive bytes, so only a
	// genuinely dead upstream stalls this long.
	streamStallTimeout = 90 * time.Second
)

// NewOpenAIClient builds a client for an OpenAI-compatible chat endpoint. token
// is optional: empty for a direct provider/Ollama, or a tela PAT when base
// points at tela cloud's managed LLM proxy. maxTokens caps the completion length
// (0 => provider default); bounding it keeps a slow local model from generating
// for minutes. The http.Client carries NO global Timeout — that's a total cap,
// wrong for streaming; Complete and CompleteStream bound themselves via context
// (a total deadline vs a per-read stall, respectively).
func NewOpenAIClient(base, model, token string, maxTokens int) *OpenAIClient {
	return &OpenAIClient{
		base:      strings.TrimRight(base, "/"),
		model:     model,
		token:     token,
		maxTokens: maxTokens,
		client:    &http.Client{},
		stall:     streamStallTimeout,
	}
}

func (c *OpenAIClient) Model() string { return c.model }

// Live is a cheap liveness probe for the AI-health prober: GET {base}/models
// (the OpenAI/Ollama/MLX model list) confirms the chat server process answers
// WITHOUT running a completion — critical here, since the answer model is often
// a large local model we must not wake just to check it's up. Any response < 500
// is reachable; a transport error (refused / DNS / timeout) or a 5xx is down.
func (c *OpenAIClient) Live(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/models", nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("llm models: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= 500 {
		return fmt.Errorf("llm models: status %d", resp.StatusCode)
	}
	return nil
}

// maxChatResponseBytes caps how much we read from the provider's response. A
// chat completion is at most a few hundred KB; 16 MiB is a generous ceiling
// that still guards against an unbounded/malicious upstream body.
const maxChatResponseBytes = 16 << 20

// chatMessage / chatRequest / chatResponse are the minimal OpenAI chat shapes we
// need. Defined here (and reused by the cloud proxy) so request/response framing
// lives in one place.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	Stream    bool          `json:"stream"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error any `json:"error"`
}

// Complete sends a system+user message pair and returns the assistant text.
// Non-streaming (stream:false) is fine for v1 — "ask your docs" wants the whole
// grounded answer, not tokens.
func (c *OpenAIClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	// Total deadline: a blocking completion has no live byte-stream to vouch for it,
	// so cap the whole exchange (was the http.Client.Timeout; now context-scoped so
	// it composes with the caller's ctx).
	ctx, cancel := context.WithTimeout(ctx, completeTotalTimeout)
	defer cancel()

	msgs := make([]chatMessage, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: userPrompt})

	body, _ := json.Marshal(chatRequest{Model: c.model, Messages: msgs, Stream: false, MaxTokens: c.maxTokens})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm chat: %w", err)
	}
	defer resp.Body.Close()

	// Cap the upstream read so a malicious/buggy provider (or attacker-controlled
	// TELA_LLM_URL in the BYO case) can't OOM the process with a huge body.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxChatResponseBytes))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm chat: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("llm decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm chat: empty completion for model %q", c.model)
	}
	return out.Choices[0].Message.Content, nil
}

// streamChunk is the per-frame delta shape for stream:true responses.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// CompleteStream runs a streaming chat completion (stream:true), invoking onToken
// for each content delta as it arrives. Because the connection streams bytes
// continuously it never sits idle, so a slow local model can't trip the proxy /
// idle timeout that a blocking completion did. onToken returning an error (e.g.
// the downstream SSE client disconnected) aborts the stream; ctx cancellation
// (client gone) stops generation too.
func (c *OpenAIClient) CompleteStream(ctx context.Context, systemPrompt, userPrompt string, onToken func(string) error) error {
	msgs := make([]chatMessage, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: userPrompt})

	// Stall watchdog: cancel the request after c.stall with no bytes — reset on
	// every read below, so a healthy stream (token deltas, or just the upstream's
	// keepalive comments during prompt-processing) never trips it, but a dead
	// connection is cut. Armed before Do so a hung header-wait is caught too.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchdog := time.AfterFunc(c.stall, cancel)
	defer watchdog.Stop()

	body, _ := json.Marshal(chatRequest{Model: c.model, Messages: msgs, Stream: true, MaxTokens: c.maxTokens})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("llm chat stream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("llm chat stream: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	// Parse the OpenAI/Ollama SSE shape: lines of `data: {…}` framing per-token
	// deltas, terminated by `data: [DONE]`. Non-data lines (blanks, comments) skip.
	br := bufio.NewReader(resp.Body)
	for {
		line, rerr := br.ReadString('\n')
		watchdog.Reset(c.stall) // bytes arrived (or the read ended) — the upstream is alive
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			if data, ok := strings.CutPrefix(trimmed, "data:"); ok {
				data = strings.TrimSpace(data)
				if data == "[DONE]" {
					return nil
				}
				var chunk streamChunk
				if json.Unmarshal([]byte(data), &chunk) == nil {
					for _, ch := range chunk.Choices {
						if ch.Delta.Content == "" {
							continue
						}
						if err := onToken(ch.Delta.Content); err != nil {
							return err
						}
					}
				}
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil
			}
			return rerr
		}
	}
}
