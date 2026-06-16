package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Image generation via an external OpenAI-compatible Images endpoint (e.g. an
// mflux / FLUX server). tahta deliberately never generates images — and tela's
// MCP agent (Claude) can't either — so tela offers generation as the operating
// agent's "own endpoint" the imagery module assumes, then stores the result as a
// page attachment ready to drop into a deck.
//
// Operational dependency, env-gated exactly like RAG/LLM: with TELA_IMAGE_GEN_URL
// unset the feature no-ops (the tool 503s). It also honours the same ai.disabled
// kill-switch as managed AI, so an admin can pause it while the backing box is
// down. Point it at a dedicated image box (e.g. mflux on :11437); from a Docker
// backend use the host's overlay IP, not its name (the RAG-embed gotcha).
type imageGen struct {
	baseURL string // OpenAI-compatible base, e.g. http://100.74.161.105:11437/v1
	model   string // default model (optional — the endpoint has its own default)
	token   string // optional bearer (the box may be credential-free)
}

func newImageGen() imageGen {
	return imageGen{
		baseURL: strings.TrimRight(os.Getenv("TELA_IMAGE_GEN_URL"), "/"),
		model:   strings.TrimSpace(os.Getenv("TELA_IMAGE_GEN_MODEL")),
		token:   strings.TrimSpace(os.Getenv("TELA_IMAGE_GEN_KEY")),
	}
}

func (g imageGen) enabled() bool { return g.baseURL != "" }

// imageGenTimeout bounds a single generation. FLUX at a few steps is ~25 s, but a
// heavier model (legible in-image text) can run minutes — so this is generous.
const imageGenTimeout = 300 * time.Second

type imageGenReq struct {
	Model          string `json:"model,omitempty"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n"`
	Size           string `json:"size,omitempty"`
	ResponseFormat string `json:"response_format"`
	Steps          int    `json:"steps,omitempty"` // mflux extension (OpenAI ignores unknown)
	Seed           int    `json:"seed,omitempty"`
}

type imageGenResp struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
		URL     string `json:"url"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// generate calls POST {base}/images/generations and returns the first image's raw
// bytes. model overrides the configured default for this call (else the endpoint's
// own default). size defaults to 16:9 1280x720 — the deck slot aspect.
func (g imageGen) generate(ctx context.Context, prompt, size, model string, steps, seed int) ([]byte, error) {
	if size == "" {
		size = "1280x720"
	}
	if model == "" {
		model = g.model
	}
	body, _ := json.Marshal(imageGenReq{
		Model: model, Prompt: prompt, N: 1, Size: size,
		ResponseFormat: "b64_json", Steps: steps, Seed: seed,
	})
	cctx, cancel := context.WithTimeout(ctx, imageGenTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, g.baseURL+"/images/generations", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	resp, err := (&http.Client{Timeout: imageGenTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image gen %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out imageGenResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("image gen: bad response: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("image gen: %s", out.Error.Message)
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("image gen: empty response")
	}
	if b := out.Data[0].B64JSON; b != "" {
		return base64.StdEncoding.DecodeString(b)
	}
	// Fallback: some servers return a URL instead of inline base64.
	if u := out.Data[0].URL; u != "" {
		ir, err := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		r2, err := (&http.Client{Timeout: 60 * time.Second}).Do(ir)
		if err != nil {
			return nil, err
		}
		defer r2.Body.Close()
		return io.ReadAll(io.LimitReader(r2.Body, 32<<20))
	}
	return nil, fmt.Errorf("image gen: response had neither b64_json nor url")
}

// imageGenEnabled reports whether deck image generation should serve: the endpoint
// is configured AND the ai.disabled kill-switch is off (same pause an admin uses
// for the rest of managed AI).
func (s *Server) imageGenEnabled() bool {
	if v, ok := s.settings.Get("ai.disabled"); ok && v == "1" {
		return false
	}
	return newImageGen().enabled()
}
