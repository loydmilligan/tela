package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	Hint     string      `json:"hint,omitempty"` // points at the guide when issues need the layout/field reference
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
	// lint reports what's wrong, not what's valid — so on any issue, point the agent
	// at the authoritative layout/field reference instead of leaving it to guess.
	if out.Errors > 0 || out.Warnings > 0 {
		out.Hint = "Call deck_authoring_guide for the full list of valid layouts and each layout's fields."
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
	m, err := deckRender(ctx, p.Body, s.deckThemeConfig(ctx, p))
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

// ── treat_deck_image ─────────────────────────────────────────────────────────

type treatImageIn struct {
	ID           int64  `json:"id" jsonschema:"id of the deck page the source image is attached to (the treated result is attached here too)"`
	AttachmentID int64  `json:"attachment_id" jsonschema:"id of an existing attachment ON THIS PAGE to treat (upload the source first with upload_attachment)"`
	Mode         string `json:"mode,omitempty" jsonschema:"duotone (palette-lock to the variant, default) or none (crop+grain only, keep the image raw)"`
	Scrim        string `json:"scrim,omitempty" jsonschema:"optional contrast scrim: left or bottom (for text over the image); omit for none"`
	Variant      string `json:"variant,omitempty" jsonschema:"tahta variant to treat for; omit to use the deck's own variant"`
}
type treatImageOut struct {
	URL      string `json:"url"`      // serve URL of the treated image attachment
	Markdown string `json:"markdown"` // ready-to-place embed snippet
	Variant  string `json:"variant"`  // variant the image was treated for
	Note     string `json:"note"`
}

// mcpTreatDeckImage runs an existing page attachment through tahta's deterministic
// treat step (tahta-imagine) for a variant and saves the result as a new attachment
// on the same page. Local + model-free — it makes an off-palette/reused image look
// tahta-grade; per the imagery module it's a FALLBACK (prefer rich on-palette images
// raw), never used on a real-colour focal subject.
func (s *Server) mcpTreatDeckImage(ctx context.Context, req *mcp.CallToolRequest, in treatImageIn) (*mcp.CallToolResult, treatImageOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), treatImageOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), treatImageOut{}, nil
	}
	p, ae := s.getPageCore(ctx, u, k, in.ID)
	if ae != nil {
		return mcpErr(ae), treatImageOut{}, nil
	}
	// Source must be an attachment on THIS page — no arbitrary URLs (no SSRF surface).
	var data []byte
	var srcName string
	err := s.DB.QueryRowContext(ctx,
		`SELECT data, name FROM space_files WHERE id = $1 AND parent_page_id = $2 AND deleted_at IS NULL`,
		in.AttachmentID, p.ID).Scan(&data, &srcName)
	if errors.Is(err, sql.ErrNoRows) {
		return mcpErr(&apiErr{http.StatusNotFound, "not_found", "no such attachment on this page — upload it first with upload_attachment"}), treatImageOut{}, nil
	}
	if err != nil {
		return mcpErr(&apiErr{http.StatusInternalServerError, "internal", "read attachment failed"}), treatImageOut{}, nil
	}
	mode := "duotone"
	if in.Mode == "none" {
		mode = "none"
	}
	scrim := ""
	if in.Scrim == "left" || in.Scrim == "bottom" {
		scrim = in.Scrim
	}
	variant := in.Variant
	if variant == "" {
		variant = s.deckThemeConfig(ctx, p).Variant
	}
	out, err := deckTreat(ctx, data, variant, mode, scrim)
	if err != nil {
		return mcpErr(&apiErr{http.StatusBadGateway, "deck_treat_failed", "could not treat image: " + err.Error()}), treatImageOut{}, nil
	}
	name := treatedName(srcName, variant)
	att, ae := s.uploadPageAttachmentCore(ctx, u, k, p.ID, name, out)
	if ae != nil {
		return mcpErr(ae), treatImageOut{}, nil
	}
	return nil, treatImageOut{
		URL:      att.URL,
		Markdown: fmt.Sprintf("![](%s)", att.URL),
		Variant:  variant,
		Note:     "Treated for the " + variant + " variant. Place it as a bg:/image: source; treatment is a fallback — prefer rich on-palette images raw, and never duotone a real-colour focal subject.",
	}, nil
}

// treatedName derives the treated attachment's filename from the source — strips
// the old extension, tags the variant, lands on .jpg (treat always emits JPEG).
func treatedName(src, variant string) string {
	base := src
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	if base == "" {
		base = "image"
	}
	return fmt.Sprintf("%s-%s.jpg", base, variant)
}

// ── generate_deck_image ──────────────────────────────────────────────────────

type genImageIn struct {
	ID     int64  `json:"id" jsonschema:"id of the deck page to attach the generated image to"`
	Prompt string `json:"prompt" jsonschema:"the image prompt — rich and specific (scene, light, texture, on-variant colours). For FLUX models append 'no text, no letters, no words' or it invents garbled type; OMIT that only when you deliberately want legible in-image text"`
	Size   string `json:"size,omitempty" jsonschema:"WxH, default 1280x720 (16:9 — the cover/bg/bleed slot aspect)"`
	Steps  int    `json:"steps,omitempty" jsonschema:"sampling steps; ~10 for hero/cover, ~4 for incidental texture (more ≈ linearly slower). Omit for the model default"`
	Seed   int    `json:"seed,omitempty" jsonschema:"optional seed for reproducibility"`
	Model  string `json:"model,omitempty" jsonschema:"optional model override (else the endpoint default)"`
	Name   string `json:"name,omitempty" jsonschema:"optional attachment filename (default deck-image-<n>.png)"`
}
type genImageOut struct {
	URL      string `json:"url"`      // serve URL of the generated image attachment
	Markdown string `json:"markdown"` // ready-to-place embed snippet
	Note     string `json:"note"`
}

// mcpGenerateDeckImage generates an image from a prompt via the configured
// image endpoint (e.g. mflux/FLUX) and saves it as a new attachment on the deck
// page, ready for a bg:/image: slot. Env-gated (TELA_IMAGE_GEN_URL) + honours the
// ai.disabled kill-switch. Per the imagery recipe: most slides need NO image —
// use it for atmosphere/concept/focal only, reuse one background, prefer rich
// on-palette images raw.
func (s *Server) mcpGenerateDeckImage(ctx context.Context, req *mcp.CallToolRequest, in genImageIn) (*mcp.CallToolResult, genImageOut, error) {
	u, k := mcpIdentity(req)
	if u == nil {
		return mcpUnauthErr(), genImageOut{}, nil
	}
	if ae := mcpRequireWrite(k); ae != nil {
		return mcpErr(ae), genImageOut{}, nil
	}
	if !s.imageGenEnabled() {
		return mcpErr(&apiErr{http.StatusServiceUnavailable, "image_gen_unavailable", "image generation isn't configured on this instance (TELA_IMAGE_GEN_URL unset) or AI is paused"}), genImageOut{}, nil
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return mcpErr(&apiErr{http.StatusBadRequest, "bad_request", "prompt is required"}), genImageOut{}, nil
	}
	p, ae := s.getPageCore(ctx, u, k, in.ID)
	if ae != nil {
		return mcpErr(ae), genImageOut{}, nil
	}
	img, err := newImageGen().generate(ctx, in.Prompt, in.Size, in.Model, in.Steps, in.Seed)
	if err != nil {
		return mcpErr(&apiErr{http.StatusBadGateway, "image_gen_failed", "could not generate image: " + err.Error()}), genImageOut{}, nil
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "deck-image.png"
	}
	att, ae := s.uploadPageAttachmentCore(ctx, u, k, p.ID, name, img)
	if ae != nil {
		return mcpErr(ae), genImageOut{}, nil
	}
	return nil, genImageOut{
		URL:      att.URL,
		Markdown: fmt.Sprintf("![](%s)", att.URL),
		Note:     "Saved as an attachment — reference it by path in a bg:/image: slot and don't regenerate on re-render. Look at the result (preview_deck); a flat slide means the prompt was too thin. Off-palette or reusing across variants? Run treat_deck_image. Never duotone a real-colour focal subject.",
	}, nil
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
