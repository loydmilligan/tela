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
// slidev-theme-tahta design system. tahta ships its OWN agent contract — AGENTS.md,
// auto-generated from its layouts.json/variants.json — covering every rule, variant,
// universal field, layout, and component (with fields/props/examples). The deck
// sidecar serves it verbatim at GET /authoring as `guide`; we wrap it in a short
// tela preamble (how decks work HERE) and serve the whole thing.
//
// This is deliberately a pass-through: the theme is the single source of truth, so
// the guide CANNOT drift as tahta gets richer — a new layout/component/field shows
// up the moment tahta is bumped, with zero changes here. Sourced at RUNTIME from
// the sidecar (the only place the theme is installed), so it always matches the
// deployed theme version — no vendoring, no codegen, no typed field list to update.

const deckAuthoringGuideURI = "tela://deck-authoring-guide"

type deckVariantSpec struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Scheme      string `json:"scheme"`
	Description string `json:"description"`
}

// deckModule is one of tahta's optional capability modules (branding, imagery, …)
// — a prompt fragment the agent pulls only when that capability is in play, so the
// always-served core guide stays lean.
type deckModule struct {
	ID   string `json:"id"`
	When string `json:"when"` // the condition under which to apply it
	Adds string `json:"adds"` // one-line summary of what it teaches
	Text string `json:"text"` // the fragment markdown
}

type deckManifestDoc struct {
	Guide    string            `json:"guide"`    // tahta's AGENTS.md, verbatim
	Variants []deckVariantSpec `json:"variants"` // structured (validation + picker)
	Modules  []deckModule      `json:"modules"`  // optional capability modules (pulled on demand)
}

var (
	deckManifestMu    sync.Mutex
	deckManifestCache *deckManifestDoc
)

// deckAuthoringManifest fetches tahta's contract from the sidecar /authoring.
// Cached for the process once it succeeds — it only changes on redeploy, which
// restarts us.
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
	return " When asked for a presentation, slides, a slide deck, or a talk (any phrasing) — not a prose doc — set the page property deck=true (and optionally variant=<style>) and write the body as slides separated by `---` using the tahta layouts; call the deck_authoring_guide tool (or read the tela://deck-authoring-guide resource) for the layouts, fields, components, and variants."
}

const deckGuideFallback = "# Authoring tela slide decks\n\n" +
	"Use this for any presentation / slides / slide deck / talk request. A tela deck is a page with property `deck: true` whose body is Slidev markdown styled by the tahta design system. " +
	"Set the style with the page property `variant`. Separate slides with `---`; each slide picks a `layout:` and fills its fields. " +
	"Do not put `theme:`/`themeConfig:` in the markdown — tela injects them. " +
	"_(The full layout/variant reference is temporarily unavailable — the deck service is unreachable.)_\n"

// telaDeckPreamble frames tahta's theme-owned guide for tela: how to MAKE a deck
// here, and the one place tela differs from tahta's stock contract — you set the
// look via page PROPS (deck/variant), never theme:/themeConfig: in the markdown.
// It supersedes the "Deck header" section of the appended contract.
func telaDeckPreamble(m *deckManifestDoc) string {
	var b strings.Builder
	b.WriteString("# Authoring tela slide decks\n\n")
	b.WriteString("Use this whenever someone asks for a **presentation, slides, a slide deck, or a talk** (any wording) — not a prose doc. A tela **deck** is a page whose body is **Slidev markdown styled by the tahta design system**. You don't write CSS, grids, or layout HTML — pick a **layout** per slide and fill its fields; tela renders it to slides.\n")
	b.WriteString("\n## How decks work in tela (read first — overrides the \"Deck header\" section below)\n")
	b.WriteString("- Make a page a deck: set the page property `deck: true` (e.g. `create_page` with `props: {\"deck\": true}`).\n")
	b.WriteString("- **Choose a `variant` deliberately for THIS deck** (page property, not in the markdown). It's the single biggest visual decision — it carries the typeface, scheme, texture, and density. Pick the one whose feel fits the deck's topic, tone, and audience; **do NOT skip it to coast on a default** (there is no default — an unset variant is an unfinished deck). Available variants:\n")
	for _, v := range m.Variants {
		b.WriteString(fmt.Sprintf("  - `%s` — %s _(%s)_\n", v.ID, v.Description, v.Scheme))
	}
	b.WriteString("  Set the brand color with the `accent` prop (hue-matched, not the exact hex — it's normalized into the variant so it stays legible), set a brand `logo` (an image URL — hero on openers + footer mark), and set `lang` (e.g. `tr`) for locale casing. (In an org space, the org's logo + accent are applied automatically if you omit them — but the variant is always yours to choose.)\n")
	b.WriteString("- **Do NOT** put `theme:`, `themeConfig:`, or a deck-header YAML block in the markdown — tela injects all of it from the page props. Ignore the \"## Deck header\" YAML in the contract below; just write the slides.\n")
	b.WriteString("- Separate slides with `---` on its own line. Each slide sets `layout:` in its frontmatter and fills that layout's fields.\n")
	if len(m.Modules) > 0 {
		b.WriteString("\n## Capability modules (pull on demand)\n")
		b.WriteString("Extra authoring guidance that applies only in specific situations. When a condition below holds, call this same `deck_authoring_guide` tool again with `module: \"<id>\"` to get that guidance, then apply it:\n")
		for _, mod := range m.Modules {
			b.WriteString(fmt.Sprintf("- `%s` — when %s. Adds: %s\n", mod.ID, mod.When, mod.Adds))
		}
	}
	b.WriteString("\nEverything below is tahta's own authoring contract (the full, current layout/component/field reference) — apply it verbatim, except the \"Deck header\" part as noted above.\n\n---\n\n")
	return b.String()
}

// deckModuleText returns a single capability module's fragment (framed with a
// short header), or "" if no module with that id exists.
func deckModuleText(m *deckManifestDoc, id string) string {
	for _, mod := range m.Modules {
		if mod.ID == id {
			return fmt.Sprintf("# tahta capability module: %s\n\n_Apply when: %s_\n\n%s", mod.ID, mod.When, mod.Text)
		}
	}
	return ""
}

// deckAuthoringGuideMarkdown = tela preamble + tahta's AGENTS.md verbatim.
func deckAuthoringGuideMarkdown(m *deckManifestDoc) string {
	if strings.TrimSpace(m.Guide) == "" {
		return deckGuideFallback
	}
	return telaDeckPreamble(m) + m.Guide
}

// deckAuthoringGuideText resolves the framed guide markdown (tela preamble +
// tahta's contract), falling back to the static note if the sidecar is down.
func deckAuthoringGuideText(ctx context.Context) string {
	if m, err := deckAuthoringManifest(ctx); err == nil {
		return deckAuthoringGuideMarkdown(m)
	}
	return deckGuideFallback
}

// mcpReadDeckAuthoringGuide serves tela://deck-authoring-guide. Fetches tahta's
// contract from the sidecar and frames it; falls back to a static note if the
// deck service is unreachable.
func (s *Server) mcpReadDeckAuthoringGuide(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     deckAuthoringGuideText(ctx),
		}},
	}, nil
}

type deckGuideIn struct {
	// Module optionally requests one of the capability modules listed in the core
	// guide (e.g. "branding", "imagery") instead of the core guide itself.
	Module string `json:"module,omitempty" jsonschema:"Optional capability module id (e.g. branding, imagery) to fetch instead of the core guide."`
}

type deckGuideOut struct {
	Guide string `json:"guide"` // markdown: tahta layouts, fields, components, variants
}

// mcpDeckAuthoringGuide is the TOOL form of the deck guide. Many MCP hosts (Claude
// Code, claude.ai/cowork) can call tools but not read `tela://` resources, so the
// guide that create_page/update_page point at was referenced-everywhere-yet-
// reachable-nowhere for them. This exposes the exact same content as a plain tool.
// With `module` set it returns that capability module (branding/imagery) instead.
func (s *Server) mcpDeckAuthoringGuide(ctx context.Context, req *mcp.CallToolRequest, in deckGuideIn) (*mcp.CallToolResult, deckGuideOut, error) {
	if u, _ := mcpIdentity(req); u == nil {
		return mcpUnauthErr(), deckGuideOut{}, nil
	}
	if id := strings.TrimSpace(in.Module); id != "" {
		if m, err := deckAuthoringManifest(ctx); err == nil {
			if text := deckModuleText(m, id); text != "" {
				return nil, deckGuideOut{Guide: text}, nil
			}
			return mcpErr(&apiErr{http.StatusNotFound, "unknown_module", "no such capability module: " + id}), deckGuideOut{}, nil
		}
		return mcpErr(&apiErr{http.StatusBadGateway, "deck_unavailable", "deck service unreachable"}), deckGuideOut{}, nil
	}
	return nil, deckGuideOut{Guide: deckAuthoringGuideText(ctx)}, nil
}
