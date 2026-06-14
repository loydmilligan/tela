package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Agent-facing DECK authoring guide. A tela deck's look comes entirely from the
// slidev-theme-tahta design system, whose machine-readable contract (layouts +
// fields + examples + components + rules + variants) is served by the deck
// sidecar at GET /authoring. We render that into a tela-framed guide so an agent
// assembles rich decks (stats/chart/compare/timeline/…) from tahta's layouts
// instead of flat bullets.
//
// Sourced at RUNTIME from the sidecar (the only place the theme is installed), so
// the guide always matches the deployed theme version — no vendoring or codegen.

const deckAuthoringGuideURI = "tela://deck-authoring-guide"

type deckField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

type deckLayoutSpec struct {
	ID      string      `json:"id"`
	UseFor  string      `json:"useFor"`
	Fields  []deckField `json:"fields"`
	Example string      `json:"example"`
}

type deckComponentSpec struct {
	Name   string `json:"name"`
	UseFor string `json:"useFor"`
}

type deckVariantSpec struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Scheme      string `json:"scheme"`
	Description string `json:"description"`
}

type deckManifestDoc struct {
	Rules      []string            `json:"rules"`
	Layouts    []deckLayoutSpec    `json:"layouts"`
	Components []deckComponentSpec `json:"components"`
	Variants   []deckVariantSpec   `json:"variants"`
}

var (
	deckManifestMu    sync.Mutex
	deckManifestCache *deckManifestDoc
)

// deckManifest fetches tahta's contract from the sidecar /authoring. Cached for
// the process once it succeeds — it only changes on redeploy, which restarts us.
func deckAuthoringManifest(ctx context.Context) (*deckManifestDoc, error) {
	deckManifestMu.Lock()
	defer deckManifestMu.Unlock()
	if deckManifestCache != nil {
		return deckManifestCache, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, deckBaseURL()+"/authoring", nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deck authoring %d", resp.StatusCode)
	}
	var m deckManifestDoc
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	deckManifestCache = &m
	return &m, nil
}

// deckAuthoringToolHint is the deck sentence appended to create_page/update_page
// (static — no sidecar dependency at call time). It tells the agent decks exist,
// how to make one, and where the full layout/variant reference lives.
func deckAuthoringToolHint() string {
	return " When asked for a presentation, slides, a slide deck, or a talk (any phrasing) — not a prose doc — set the page property deck=true (and optionally variant=<style>) and write the body as slides separated by `---` using the tahta layouts; read the tela://deck-authoring-guide resource for the layouts, fields, components, and variants."
}

const deckGuideFallback = "# Authoring tela slide decks\n\n" +
	"Use this for any presentation / slides / slide deck / talk request. A tela deck is a page with property `deck: true` whose body is Slidev markdown styled by the tahta design system. " +
	"Set the style with the page property `variant`. Separate slides with `---`; each slide picks a `layout:` and fills its fields. " +
	"Do not put `theme:`/`themeConfig:` in the markdown — tela injects them. " +
	"_(The full layout/variant reference is temporarily unavailable — the deck service is unreachable.)_\n"

// deckAuthoringGuideMarkdown renders tahta's contract as a tela-framed guide.
func deckAuthoringGuideMarkdown(m *deckManifestDoc) string {
	fence := "````"
	var b strings.Builder
	b.WriteString("# Authoring tela slide decks\n\n")
	b.WriteString("Use this whenever someone asks for a **presentation, slides, a slide deck, or a talk** (any wording). A tela **deck** is a page whose body is **Slidev markdown styled by the tahta design system**. You don't write CSS, grids, or layout HTML — pick a **layout** per slide and fill its fields; tela renders it to slides.\n")

	b.WriteString("\n## How decks work in tela\n")
	b.WriteString("- Make a page a deck by setting the page property `deck: true` (e.g. `create_page` with `props: {\"deck\": true}`).\n")
	b.WriteString("- Set the visual style with the page property `variant` — **not** a theme in the markdown. Available variants:\n")
	for _, v := range m.Variants {
		b.WriteString(fmt.Sprintf("  - `%s` — %s _(%s)_\n", v.ID, v.Description, v.Scheme))
	}
	b.WriteString("- **Do NOT** put `theme:` or `themeConfig:` in the markdown — tela injects them from the page props.\n")
	b.WriteString("- Separate slides with `---` on its own line. Each slide sets `layout:` in its frontmatter and fills that layout's fields.\n")

	if len(m.Rules) > 0 {
		b.WriteString("\n## Rules\n")
		for _, r := range m.Rules {
			b.WriteString("- " + r + "\n")
		}
	}

	b.WriteString("\n## Layouts — pick the one that matches the content shape\n")
	for _, l := range m.Layouts {
		b.WriteString("\n### `" + l.ID + "`")
		if l.UseFor != "" {
			b.WriteString(" — " + l.UseFor)
		}
		b.WriteString("\n")
		if len(l.Fields) > 0 {
			var fs []string
			for _, f := range l.Fields {
				name := f.Name
				if f.Required {
					name += "*"
				}
				fs = append(fs, name)
			}
			b.WriteString("Fields: " + strings.Join(fs, ", ") + "  _(\\* = required)_\n")
		}
		if l.Example != "" {
			b.WriteString(fence + "yaml\n" + l.Example + "\n" + fence + "\n")
		}
	}

	if len(m.Components) > 0 {
		b.WriteString("\n## Components — use inline in `default`/`statement` bodies\n")
		for _, c := range m.Components {
			b.WriteString("- `<" + c.Name + ">`")
			if c.UseFor != "" {
				b.WriteString(" — " + c.UseFor)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// mcpReadDeckAuthoringGuide serves tela://deck-authoring-guide. Fetches tahta's
// contract from the sidecar and renders it; falls back to a static note if the
// deck service is unreachable.
func (s *Server) mcpReadDeckAuthoringGuide(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	text := deckGuideFallback
	if m, err := deckAuthoringManifest(ctx); err == nil {
		text = deckAuthoringGuideMarkdown(m)
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     text,
		}},
	}, nil
}
