package main

import (
	"strings"
	"testing"
)

// refOverview mirrors atlas renderOverview's exact heading/table layout
// (internal/engine/deliver.go) for a source root page.
const refOverview = "# acme — documentation\n\n" +
	"> [!NOTE]\n> Generated and maintained by **atlas** — do not edit by hand; it is overwritten on each run.\n" +
	"> Source: `github.com/acme/acme` · commit `abc123def456`\n\n" +
	"## Run\n\n" +
	"| Generated | Duration | Model |\n|---|---|---|\n| 2026-06-26 10:00 | 3m 4s | gpt-x + embed-y |\n\n" +
	"| Files | Surface | Chunks | Pages |\n|---|---|---|---|\n| 12 | 45 | 120 | 8 |\n\n" +
	"| Chat tokens | Embed tokens | Calls | Cost |\n|---|---|---|---|\n| 1,234 in / 567 out | 8,900 | 10 chat · 3 embed | $0.0123 |\n\n" +
	"## Coverage\n\n" +
	"| Surface covered | Must-cover | Citations | Diagrams |\n|---|---|---|---|\n" +
	"| 40/45 (89%) | 22/24 (92%) | 30 (1 unresolved) | 5 |\n\n" +
	"### Undocumented surface (2)\n\n" +
	"- `func` Foo — `pkg/a.go:12`\n" +
	"- `type` Bar — `pkg/b.go:34`\n\n" +
	"## Surface inventory\n\n" +
	"| Kind | Count |\n|---|---|\n| func | 30 |\n| type | 15 |\n\n" +
	"## Pages\n\n" +
	"- Overview\n- Internals\n"

func TestParseOverview(t *testing.T) {
	c := parseOverview(refOverview)

	if c.Covered != 40 || c.Total != 45 || c.CoveredPct != 89 {
		t.Errorf("surface-covered: got %d/%d (%.0f%%)", c.Covered, c.Total, c.CoveredPct)
	}
	if c.MustCovered != 22 || c.MustTot != 24 || c.MustPct != 92 {
		t.Errorf("must-cover: got %d/%d (%.0f%%)", c.MustCovered, c.MustTot, c.MustPct)
	}
	if c.Citations != 30 || c.Unresolv != 1 {
		t.Errorf("citations: got %d (%d unresolved)", c.Citations, c.Unresolv)
	}
	if c.Diagrams != 5 {
		t.Errorf("diagrams: got %d, want 5", c.Diagrams)
	}
	if c.Inventory["func"] != 30 || c.Inventory["type"] != 15 {
		t.Errorf("inventory: got %#v", c.Inventory)
	}
	if len(c.Inventory) != 2 {
		t.Errorf("inventory size: got %d, want 2", len(c.Inventory))
	}
	if len(c.Gaps) != 2 {
		t.Fatalf("gaps: got %d, want 2 (%#v)", len(c.Gaps), c.Gaps)
	}
	if c.Gaps[0] != "`func` Foo — `pkg/a.go:12`" {
		t.Errorf("gap[0]: got %q", c.Gaps[0])
	}
}

func TestParseOverviewMissingSections(t *testing.T) {
	// A child doc page (no overview) parses to a zero-value coverage, no panic.
	c := parseOverview("# Internals\n\nSome prose with no coverage table.\n")
	if c.Covered != 0 || c.Diagrams != 0 || len(c.Gaps) != 0 || len(c.Inventory) != 0 {
		t.Errorf("non-overview body should parse empty: %#v", c)
	}
}

func TestDiffCoverageMaterial(t *testing.T) {
	// Same as ref but: must-cover dropped 92%→84% (>2pt), one new gap, an
	// inventory count drift, +1 unresolved citation. Each alone is material.
	regressed := "## Coverage\n\n" +
		"| Surface covered | Must-cover | Citations | Diagrams |\n|---|---|---|---|\n" +
		"| 40/45 (89%) | 20/24 (84%) | 30 (2 unresolved) | 5 |\n\n" +
		"### Undocumented surface (3)\n\n" +
		"- `func` Foo — `pkg/a.go:12`\n" +
		"- `type` Bar — `pkg/b.go:34`\n" +
		"- `func` Baz — `pkg/c.go:7`\n\n" +
		"## Surface inventory\n\n" +
		"| Kind | Count |\n|---|---|\n| func | 28 |\n| type | 15 |\n"

	d := diffCoverage("acme", parseOverview(refOverview), parseOverview(regressed), 2.0)
	if !d.Material {
		t.Fatalf("expected material divergence, got none: %#v", d)
	}
	if d.MustPctΔ != -8 {
		t.Errorf("must-cover delta: got %.0f, want -8", d.MustPctΔ)
	}
	if d.UnresolvedΔ != 1 {
		t.Errorf("unresolved delta: got %d, want 1", d.UnresolvedΔ)
	}
	if len(d.GapsOnlyNew) != 1 || d.GapsOnlyNew[0] != "`func` Baz — `pkg/c.go:7`" {
		t.Errorf("gaps only in new: got %#v", d.GapsOnlyNew)
	}
	if len(d.InvCountDiff) != 1 {
		t.Errorf("inventory count diff: got %#v", d.InvCountDiff)
	}
}

func TestDiffCoverageWithinTolerance(t *testing.T) {
	// Identical tables → no divergence; LLM-noise wording elsewhere is irrelevant
	// to the coverage diff.
	d := diffCoverage("acme", parseOverview(refOverview), parseOverview(refOverview), 2.0)
	if d.Material {
		t.Errorf("identical coverage should not be material: %#v", d)
	}

	// A 1-point covered drift stays under the 2pt tolerance (everything else,
	// inventory + gaps included, identical to ref).
	slightly := strings.Replace(refOverview, "40/45 (89%)", "40/45 (88%)", 1)
	d2 := diffCoverage("acme", parseOverview(refOverview), parseOverview(slightly), 2.0)
	if d2.Material {
		t.Errorf("1pt drift under tolerance should not be material: %#v", d2)
	}
}

func TestTokenJaccard(t *testing.T) {
	if j := tokenJaccard("the quick brown fox", "the quick brown fox"); j != 1 {
		t.Errorf("identical: got %v, want 1", j)
	}
	if j := tokenJaccard("", ""); j != 1 {
		t.Errorf("both empty: got %v, want 1", j)
	}
	if j := tokenJaccard("alpha beta", "gamma delta"); j != 0 {
		t.Errorf("disjoint: got %v, want 0", j)
	}
	// {a,b,c,d} vs {a,b,c,e}: inter=3 union=5 → 0.6
	j := tokenJaccard("a b c d", "a b c e")
	if j < 0.59 || j > 0.61 {
		t.Errorf("partial overlap: got %v, want ~0.6", j)
	}
	// Punctuation/case differences are normalized away.
	if j := tokenJaccard("Hello, World!", "hello world"); j != 1 {
		t.Errorf("case/punct insensitive: got %v, want 1", j)
	}
}

func TestDiffStructureAndPageSet(t *testing.T) {
	root := func(title string) *node {
		return &node{Title: title, Body: refOverview, Children: []*node{
			{Title: "Overview"}, {Title: "Internals"},
		}}
	}
	ref := flatten([]*node{root("acme")})

	// new: same root+coverage, children reordered, plus an extra page. The
	// reorder is over the SHARED titles (Overview/Internals) so it isn't masked
	// by the page-set delta.
	neu := flatten([]*node{{Title: "acme", Body: refOverview, Children: []*node{
		{Title: "Internals"}, {Title: "Overview"}, {Title: "Extra"},
	}}})

	r := diff(ref, neu, 0.6, 2.0)
	if r.PageSetOK {
		t.Errorf("expected page-set mismatch (Extra only in new)")
	}
	if len(r.MissingInNew) != 0 {
		t.Errorf("missing in new: got %#v", r.MissingInNew)
	}
	if len(r.ExtraInNew) != 1 || r.ExtraInNew[0] != "Extra" {
		t.Errorf("extra in new: got %#v", r.ExtraInNew)
	}
	if r.StructureOK {
		t.Errorf("expected child-order structure diff")
	}
	if !r.Material {
		t.Errorf("page-set + structure mismatch must be material")
	}
}

func TestDiffClean(t *testing.T) {
	mk := func() []*node {
		return []*node{{Title: "acme", Body: refOverview, Children: []*node{
			{Title: "Overview", Body: "alpha beta gamma delta"},
			{Title: "Internals", Body: "one two three four"},
		}}}
	}
	r := diff(flatten(mk()), flatten(mk()), 0.6, 2.0)
	if r.Material {
		t.Errorf("identical spaces must not be material: %#v", r)
	}
	if !r.PageSetOK || !r.StructureOK || !r.CoverageOK {
		t.Errorf("all dimensions should pass: %#v", r)
	}
	if len(r.BodyDrift) != 0 {
		t.Errorf("identical bodies → no drift: %#v", r.BodyDrift)
	}
}
