package llm

import (
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
	base   string // includes the /v1 (or equivalent) prefix; we append /chat/completions
	model  string
	token  string // optional bearer; set when base points at tela's managed endpoint
	client *http.Client
}

// NewOpenAIClient builds a client for an OpenAI-compatible chat endpoint. token
// is optional: empty for a direct provider/Ollama, or a tela PAT when base
// points at tela cloud's managed LLM proxy.
func NewOpenAIClient(base, model, token string) *OpenAIClient {
	return &OpenAIClient{
		base:   strings.TrimRight(base, "/"),
		model:  model,
		token:  token,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *OpenAIClient) Model() string { return c.model }

// chatMessage / chatRequest / chatResponse are the minimal OpenAI chat shapes we
// need. Defined here (and reused by the cloud proxy) so request/response framing
// lives in one place.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
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
	msgs := make([]chatMessage, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: userPrompt})

	body, _ := json.Marshal(chatRequest{Model: c.model, Messages: msgs, Stream: false})
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

	raw, _ := io.ReadAll(resp.Body)
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
