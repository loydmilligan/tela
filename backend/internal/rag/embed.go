package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// OllamaEmbedder calls an Ollama server's /api/embed endpoint. "Hosted Ollama"
// in tela's case is tardis on the tailnet (http://tardis:11434), model
// mxbai-embed-large (1024-d). Swapping to a hosted OpenAI-compatible provider
// is a sibling type implementing Embedder, not a change here.
type OllamaEmbedder struct {
	base   string
	model  string
	client *http.Client
}

func NewOllamaEmbedder(base, model string) *OllamaEmbedder {
	return &OllamaEmbedder{
		base:   strings.TrimRight(base, "/"),
		model:  model,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (e *OllamaEmbedder) Model() string { return e.model }

// maxEmbedChars is the initial rune cap on embed input — a first-pass guard so
// most chunks embed in one shot. It is intentionally loose because token density
// varies wildly (symbol-heavy markdown can be <2 chars/token), so a char cap
// can't reliably predict the model's 512-token limit. The real guarantee is the
// shrink-retry loop in Embed.
const maxEmbedChars = 1600

// embedMinChars is the floor below which we stop shrinking and surface the error
// rather than embed a near-empty fragment.
const embedMinChars = 64

func clampRunes(text string, n int) string {
	r := []rune(text)
	if len(r) <= n {
		return text
	}
	return string(r[:n])
}

// Embed returns the embedding for text via POST /api/embed {model, input}. The
// Ollama API returns {"embeddings": [[...]]}; we send one string and take row 0.
//
// mxbai-embed-large rejects (HTTP 400 — it does NOT silently truncate) input
// past its 512-token context, and token density isn't predictable from char
// count. So on a context-overflow 400 we shrink the input 25% and retry until it
// fits (down to embedMinChars). Only the embedded text shrinks; the stored chunk
// content + lexical index keep the full text, and the contextual prefix sits at
// the head, so we only ever drop the tail of an over-long section.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	input := clampRunes(text, maxEmbedChars)
	for {
		vec, overflow, err := e.embedOnce(ctx, input)
		if err == nil {
			return vec, nil
		}
		n := len([]rune(input))
		if !overflow || n <= embedMinChars {
			return nil, err
		}
		input = clampRunes(input, n*3/4)
	}
}

// embedOnce does a single embed request. overflow is true when the model
// rejected the input for exceeding its context window (the retryable case).
func (e *OllamaEmbedder) embedOnce(ctx context.Context, input string) (vec []float32, overflow bool, err error) {
	body, _ := json.Marshal(map[string]any{"model": e.model, "input": input})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.base+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		over := resp.StatusCode == http.StatusBadRequest && strings.Contains(string(raw), "context length")
		return nil, over, fmt.Errorf("ollama embed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
		Error      string      `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false, fmt.Errorf("ollama decode: %w", err)
	}
	if out.Error != "" {
		return nil, false, fmt.Errorf("ollama embed: %s", out.Error)
	}
	if len(out.Embeddings) == 0 || len(out.Embeddings[0]) == 0 {
		return nil, false, fmt.Errorf("ollama embed: empty embedding for model %q", e.model)
	}
	return out.Embeddings[0], false, nil
}

// vecLiteral formats a float32 slice as a pgvector text literal ("[0.1,0.2]").
// pgvector parses this on a ::vector cast, so we never need a driver-level type
// or an extra dependency — the value crosses database/sql as a plain string.
func vecLiteral(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 8)
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
