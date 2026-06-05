package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// Embed returns the embedding for text via POST /api/embed {model, input}. The
// current Ollama API returns {"embeddings": [[...]]} (one row per input); we
// send a single string and take the first row.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": e.model, "input": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.base+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed: status %d", resp.StatusCode)
	}

	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
		Error      string      `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama decode: %w", err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("ollama embed: %s", out.Error)
	}
	if len(out.Embeddings) == 0 || len(out.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("ollama embed: empty embedding for model %q", e.model)
	}
	return out.Embeddings[0], nil
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
