package engine

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/atlas/core"
)

// validateStage audits the generated docs objectively: completeness against the
// spine (every surface item mentioned?), citation resolution (do file:line refs
// point at real lines?), and a Mermaid count. This is the measurement DeepWiki
// can't do — and what the repair loop acts on.
type validateStage struct{}

func (validateStage) Name() core.StageName { return core.StageValidate }

func (validateStage) Run(ctx context.Context, rc *RunContext) error {
	rc.Coverage = computeCoverage(rc)
	_ = rc.Store.SaveRunCoverage(rc.Run.ID, rc.Coverage)
	c := rc.Coverage
	rc.Info("coverage: %.0f%% surface (%d/%d) · must-cover %.0f%% (%d/%d) · %d citations (%d unresolved) · %d diagrams",
		100*c.Rate(), c.Covered, c.Total, 100*c.MustRate(), c.MustCovered, c.MustTotal, c.Citations, c.BadCitations, c.Mermaid)
	if len(c.Gaps) > 0 {
		rc.Warn("%d uncovered surface item(s): %s", len(c.Gaps), gapSample(c.Gaps, 12))
	}
	return nil
}

// computeCoverage is the pure audit, reused by the repair loop.
func computeCoverage(rc *RunContext) core.Coverage {
	allBodies := joinBodies(rc.Art.Pages)
	var c core.Coverage
	for _, it := range rc.Art.Spine {
		c.Total++
		must := core.MustCoverKinds[it.Kind]
		if must {
			c.MustTotal++
		}
		if strings.Contains(allBodies, coverKey(it)) {
			c.Covered++
			if must {
				c.MustCovered++
			}
		} else {
			c.Gaps = append(c.Gaps, core.Gap{Kind: it.Kind, Name: it.Name, File: it.File, Line: it.Line})
		}
	}
	c.Citations, c.BadCitations, c.BadCites = auditCitations(rc, allBodies)
	c.Mermaid = strings.Count(allBodies, "```mermaid")
	c.MermaidValid, c.MermaidInvalid, c.MermaidGaps = validateMermaid(rc.Art.Pages)
	return c
}

// knownMermaidHeaders are the diagram-type keywords a mermaid block must open
// with. A block whose first non-blank line starts with none of these is flagged.
var knownMermaidHeaders = []string{
	"graph", "flowchart", "sequenceDiagram", "classDiagram", "stateDiagram",
	"erDiagram", "gantt", "journey", "pie", "mindmap", "timeline",
}

// validateMermaid extracts every ```mermaid block across the pages and flags the
// obviously-broken ones: empty, missing a known diagram header, or with clearly
// unbalanced brackets. This is a cheap heuristic — full validation would need
// mmdc (mermaid-cli); a proper render check is a follow-up.
func validateMermaid(pages []core.Page) (valid, invalid int, gaps []core.MermaidGap) {
	for _, p := range pages {
		for _, block := range mermaidBlocks(p.Body) {
			if err := checkMermaid(block); err != "" {
				invalid++
				gaps = append(gaps, core.MermaidGap{Page: p.Slug, Err: err})
			} else {
				valid++
			}
		}
	}
	return valid, invalid, gaps
}

// mermaidBlocks returns the inner contents of every fenced ```mermaid block.
func mermaidBlocks(body string) []string {
	var out []string
	lines := strings.Split(body, "\n")
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "```mermaid" {
			var b strings.Builder
			i++
			for i < len(lines) && strings.TrimSpace(lines[i]) != "```" {
				b.WriteString(lines[i])
				b.WriteByte('\n')
				i++
			}
			out = append(out, b.String())
		}
	}
	return out
}

// checkMermaid returns a short reason a block is invalid, or "" if it looks ok.
func checkMermaid(block string) string {
	first := ""
	for _, ln := range strings.Split(block, "\n") {
		if s := strings.TrimSpace(ln); s != "" {
			first = s
			break
		}
	}
	if first == "" {
		return "empty diagram"
	}
	hasHeader := false
	for _, h := range knownMermaidHeaders {
		if strings.HasPrefix(first, h) {
			hasHeader = true
			break
		}
	}
	if !hasHeader {
		return fmt.Sprintf("missing diagram header (got %q)", firstToken(first))
	}
	if !balanced(block) {
		return "unbalanced brackets"
	}
	if lbl := unquotedLabelOffender(block); lbl != "" {
		return fmt.Sprintf("unquoted special char in node label %q (quote it: A[\"…\"])", lbl)
	}
	return ""
}

// unquotedLabelOffender returns the first node label that carries a raw ':' '('
// or ')' OUTSIDE double quotes — the exact tokens mermaid's parser chokes on in
// an unquoted label (e.g. A[Schema: Types]). A quoted label (A["Schema: Types"])
// is fine and passes. This is still a heuristic — full validation needs mmdc
// (mermaid-cli) to actually parse; we only catch the common label breakage.
//
// It scans each `id[...]` / `id(...)` / `id{...}` node-label span: the opener
// must follow an identifier char (so a bare '(' in prose isn't a node), and the
// inner text is checked for an unquoted offender.
func unquotedLabelOffender(block string) string {
	closers := map[byte]byte{'[': ']', '(': ')', '{': '}'}
	for _, line := range strings.Split(block, "\n") {
		for i := 0; i < len(line); i++ {
			open := line[i]
			close, isOpen := closers[open]
			if !isOpen || i == 0 || !isIdentChar(line[i-1]) {
				continue
			}
			// find the matching closer for this label (mermaid labels don't nest).
			end := strings.IndexByte(line[i+1:], close)
			if end < 0 {
				break
			}
			inner := line[i+1 : i+1+end]
			if labelHasUnquotedSpecial(inner) {
				return strings.TrimSpace(inner)
			}
			i += end + 1
		}
	}
	return ""
}

// labelHasUnquotedSpecial reports whether s contains a ':' '(' or ')' that is not
// inside a double-quoted span.
func labelHasUnquotedSpecial(s string) bool {
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			inQuote = !inQuote
		case !inQuote && (c == ':' || c == '(' || c == ')'):
			return true
		}
	}
	return false
}

func isIdentChar(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// firstToken is the leading whitespace-delimited word, for error messages.
func firstToken(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return s
}

// balanced reports whether []/()/{} are pairwise balanced across the block.
func balanced(s string) bool {
	pairs := map[rune]rune{')': '(', ']': '[', '}': '{'}
	var stack []rune
	for _, r := range s {
		switch r {
		case '(', '[', '{':
			stack = append(stack, r)
		case ')', ']', '}':
			if len(stack) == 0 || stack[len(stack)-1] != pairs[r] {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}
	return len(stack) == 0
}

func joinBodies(pages []core.Page) string {
	var b strings.Builder
	for _, p := range pages {
		b.WriteString(p.Body)
		b.WriteByte('\n')
	}
	return b.String()
}

// coverKey is the distinctive token that proves an item is documented: the path
// for a route, the bare symbol for an export, the literal name otherwise.
func coverKey(it core.SpineItem) string {
	switch it.Kind {
	case core.KindRoute:
		f := strings.Fields(it.Name)
		return f[len(f)-1] // strip the HTTP verb
	case core.KindExport:
		f := strings.Fields(it.Name)
		return f[len(f)-1] // strip "func"/"type"/"class"
	default:
		return it.Name
	}
}

// citeRe matches a `path:line` (or `path:l1-l2`) citation. The path char-class
// includes `@` so scoped paths (e.g. vendor/@graphql-mesh/types.d.ts) match as
// one unit — without it only the post-@ tail matched, splitting the link.
var citeRe = regexp.MustCompile(`([\w@./-]+\.\w+):(\d+)(?:-(\d+))?`)

const maxBadCites = 20 // cap on the unresolved-citation sample we persist

// resolveCite maps a cited path to a real corpus file path and its line count.
// It tries an exact match first, then a suffix match (the model may cite a
// basename or a trimmed path). The shared resolution used by both the citation
// audit and the publish-time linkifier.
func resolveCite(files []core.File, path string) (realPath string, lines int, ok bool) {
	for _, f := range files {
		if f.Path == path {
			return f.Path, f.Lines, true
		}
	}
	for _, f := range files {
		if strings.HasSuffix(f.Path, "/"+path) || strings.HasSuffix(f.Path, path) {
			return f.Path, f.Lines, true
		}
	}
	return "", 0, false
}

// auditCitations counts file:line references, how many fail to resolve to a real
// file/line in the repo, and collects a deduped, capped sample of the unresolved
// ones for surfacing in the overview/UI.
func auditCitations(rc *RunContext, bodies string) (total, bad int, badCites []string) {
	seen := map[string]bool{}
	for _, m := range citeRe.FindAllStringSubmatch(bodies, -1) {
		path, ln := m[1], m[2]
		n, _ := strconv.Atoi(ln)
		_, fl, ok := resolveCite(rc.Art.Files, path)
		total++
		if !ok || n < 1 || n > fl {
			bad++
			cite := m[0]
			if !seen[cite] && len(badCites) < maxBadCites {
				seen[cite] = true
				badCites = append(badCites, cite)
			}
		}
	}
	return total, bad, badCites
}

func gapSample(gaps []core.Gap, n int) string {
	parts := make([]string, 0, n)
	for i, g := range gaps {
		if i >= n {
			parts = append(parts, "…")
			break
		}
		parts = append(parts, string(g.Kind)+":"+g.Name)
	}
	return strings.Join(parts, ", ")
}
