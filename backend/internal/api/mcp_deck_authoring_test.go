package api

import (
	"strings"
	"testing"
)

// Sample mirrors the shape the deck sidecar serves at /authoring: tahta's own
// AGENTS.md verbatim (`guide`) + the structured variant catalog.
func sampleDeckManifest() *deckManifestDoc {
	return &deckManifestDoc{
		Guide: "# tahta — authoring contract for agents\n\n" +
			"## Rules\n1. One idea per slide.\n\n" +
			"## Deck header\n```yaml\ntheme: slidev-theme-tahta\n```\n\n" +
			"## Layouts\n### `stats`\n2–4 hero numbers.\n- `stats` (array, **required**)\n" +
			"```yaml\nlayout: stats\nstats:\n  - { value: 80, unit: \"%\" }\n```\n\n" +
			"## Components\n- **`<Stat>`** — big number\n",
		Variants: []deckVariantSpec{
			{ID: "editorial", Label: "Editorial", Scheme: "dark", Description: "serif"},
			{ID: "brutalist", Label: "Brutalist", Scheme: "dark", Description: "mono"},
		},
		Modules: []deckModule{
			{ID: "branding", When: "the deck has a brand", Adds: "map a brand brief to a deck", Text: "# branding\nPick the variant that fits.\n"},
		},
	}
}

func TestDeckAuthoringGuideMarkdown(t *testing.T) {
	g := deckAuthoringGuideMarkdown(sampleDeckManifest())
	for _, want := range []string{
		"deck: true",          // how to make a deck (page prop) — tela preamble
		"variant",             // style is a page prop
		"`editorial`",         // variant disclosed (from structured catalog)
		"`stats`",             // layout disclosed (from tahta guide, verbatim)
		"One idea per slide.", // rules carried through verbatim
		"`<Stat>`",            // component carried through verbatim
		"layout: stats",       // per-layout example carried through verbatim
	} {
		if !strings.Contains(g, want) {
			t.Errorf("deck guide missing %q", want)
		}
	}
	// Must steer agents AWAY from theme headmatter (tela injects it) — overriding
	// tahta's stock "Deck header" instruction.
	if !strings.Contains(g, "Do NOT") {
		t.Error("deck guide should warn against theme/themeConfig in markdown")
	}
	// Capability modules are advertised in the core guide (pull-on-demand), not
	// inlined — keeps the core lean.
	for _, want := range []string{"Capability modules", "`branding`", `module: "<id>"`} {
		if !strings.Contains(g, want) {
			t.Errorf("deck guide missing module advertisement %q", want)
		}
	}
	if strings.Contains(g, "Pick the variant that fits.") {
		t.Error("module body should NOT be inlined into the core guide")
	}
}

// deckModuleText returns a single module's fragment (framed), or "" for unknown.
func TestDeckModuleText(t *testing.T) {
	m := sampleDeckManifest()
	got := deckModuleText(m, "branding")
	for _, want := range []string{"capability module: branding", "the deck has a brand", "Pick the variant that fits."} {
		if !strings.Contains(got, want) {
			t.Errorf("module text missing %q; got %q", want, got)
		}
	}
	if deckModuleText(m, "nope") != "" {
		t.Error("unknown module id should return empty string")
	}
}

// An older theme without AGENTS.md (empty guide) must fall back, not emit a
// preamble pointing at a missing contract.
func TestDeckAuthoringGuideMarkdown_EmptyGuideFallsBack(t *testing.T) {
	g := deckAuthoringGuideMarkdown(&deckManifestDoc{Variants: nil})
	if g != deckGuideFallback {
		t.Errorf("empty guide should fall back; got %q", g)
	}
}

func TestDeckAuthoringToolHint(t *testing.T) {
	h := deckAuthoringToolHint()
	for _, want := range []string{"deck=true", "tela://deck-authoring-guide"} {
		if !strings.Contains(h, want) {
			t.Errorf("deck tool hint missing %q", want)
		}
	}
}
