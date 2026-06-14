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
