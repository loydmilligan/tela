package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Deck MCP tools that close the agent authoring loop: lint_deck (structural
// validation against the tahta contract — the "am I wrong?" feedback) and
// preview_deck (return the rendered slides as images — so the agent can SEE its
// output instead of authoring blind). Both are page-id scoped and reuse the same
// membership gate as the other page tools.

// ── lint_deck ────────────────────────────────────────────────────────────────

type lintDeckIn struct {
	ID int64 `json:"id" jsonschema:"id of the deck page to validate"`
}
type lintIssue struct {
	Slide   int    `json:"slide"`
	Level   string `json:"level"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}
type lintDeckOut struct {
	OK       bool        `json:"ok"`
	Errors   int         `json:"errors"`
	Warnings int         `json:"warnings"`
	Issues   []lintIssue `json:"issues"`
}

func (s *Server) mcpLintDeck(ctx context.Context, req *mcp.CallToolRequest, in lintDeckIn) (*mcp.CallToolResult, lintDeckOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), lintDeckOut{}, nil
	}
	p, ae := s.getPageCore(ctx, u, k, in.ID)
	if ae != nil {
		return mcpErr(ae), lintDeckOut{}, nil
	}
	resp, err := deckPost(ctx, "/lint", p.Body, deckConfig{})
	if err != nil {
		return mcpErr(&apiErr{http.StatusBadGateway, "deck_unavailable", "deck service unavailable"}), lintDeckOut{}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return mcpErr(&apiErr{http.StatusBadGateway, "deck_lint_failed", "could not lint deck"}), lintDeckOut{}, nil
	}
	var out lintDeckOut
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return mcpErr(&apiErr{http.StatusInternalServerError, "internal", "bad lint response"}), lintDeckOut{}, nil
	}
	return nil, out, nil
}

// ── preview_deck ─────────────────────────────────────────────────────────────

type previewDeckIn struct {
	ID     int64 `json:"id" jsonschema:"id of the deck page to render and preview"`
	Slides []int `json:"slides,omitempty" jsonschema:"optional 1-based frame numbers to preview; omit for the first few"`
}

const previewDeckMax = 10 // cap images per call to keep the tool result a sane size

func (s *Server) mcpPreviewDeck(ctx context.Context, req *mcp.CallToolRequest, in previewDeckIn) (*mcp.CallToolResult, any, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), nil, nil
	}
	p, ae := s.getPageCore(ctx, u, k, in.ID)
	if ae != nil {
		return mcpErr(ae), nil, nil
	}
	m, err := deckRender(ctx, p.Body, deckThemeConfig(p))
	if err != nil {
		return mcpErr(&apiErr{http.StatusBadGateway, "deck_render_failed", "could not render deck"}), nil, nil
	}
	idxs := pickPreviewFrames(in.Slides, m.Count)
	content := []mcp.Content{&mcp.TextContent{
		Text: fmt.Sprintf("%q rendered with the %q variant — %d frame(s); showing %d.", p.Title, m.Variant, m.Count, len(idxs)),
	}}
	for _, i := range idxs {
		png, err := s.fetchDeckFrame(ctx, m.Slides[i-1]) // m.Slides are raw sidecar /d/<id>/<file> paths
		if err != nil {
			continue
		}
		content = append(content, &mcp.ImageContent{Data: png, MIMEType: "image/png"})
	}
	return &mcp.CallToolResult{Content: content}, nil, nil
}

// pickPreviewFrames resolves the requested 1-based frames (clamped, deduped,
// capped) or the first previewDeckMax when none requested.
func pickPreviewFrames(want []int, count int) []int {
	if count <= 0 {
		return nil
	}
	var out []int
	seen := map[int]bool{}
	add := func(n int) {
		if n >= 1 && n <= count && !seen[n] && len(out) < previewDeckMax {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(want) > 0 {
		for _, n := range want {
			add(n)
		}
		return out
	}
	for n := 1; n <= count; n++ {
		add(n)
	}
	return out
}

// fetchDeckFrame pulls a rendered frame's PNG bytes from the sidecar.
func (s *Server) fetchDeckFrame(ctx context.Context, sidecarPath string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, deckBaseURL()+sidecarPath, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("frame %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}
