package api

import (
	"fmt"
	"regexp"
	"strings"
)

// Surgical sub-document editing (T1): instead of an agent re-sending a whole page
// to change one part, get_page format:"map" returns just the heading outline, and
// patch_page edits ONE section by its heading path. Both rest on pageOutline, a
// fence-aware markdown section parser — a '#' inside a ``` block is not a heading.
// patch_page reassembles the body and writes it through the normal update path, so
// revisions / reindex / agreement / provenance all happen exactly as for any edit.

var mapHeadingRE = regexp.MustCompile(`^(#{1,6})\s+(.*\S)\s*$`)

// pageSection is one heading and the span of lines it governs: the heading line
// through the line before the next heading of the SAME OR HIGHER level — i.e. the
// whole section, subsections included.
type pageSection struct {
	Level   int    `json:"level"`
	Heading string `json:"heading"`
	Path    string `json:"path"`              // "Parent > Child" breadcrumb — the patch_page target
	Preview string `json:"preview,omitempty"` // a short prose snippet of the section's own content

	headingLine int // line index of the heading
	bodyStart   int // first line after the heading
	end         int // exclusive: next same-or-higher heading, or EOF
}

// pageOutline parses a markdown body into its heading sections in document order,
// fence-aware. Line spans are half-open [headingLine,end).
func pageOutline(body string) []pageSection {
	lines := strings.Split(body, "\n")
	type hd struct {
		idx, level    int
		heading, path string
	}
	var heads []hd
	var stack []string // heading text per level (index = level-1)
	inFence := false
	fenceTok := ""
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			tok := trimmed[:3]
			if !inFence {
				inFence, fenceTok = true, tok
			} else if tok == fenceTok {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		m := mapHeadingRE.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		level := len(m[1])
		title := strings.TrimSpace(m[2])
		if len(stack) >= level {
			stack = stack[:level-1]
		} else {
			for len(stack) < level-1 {
				stack = append(stack, "")
			}
		}
		stack = append(stack, title)
		parts := make([]string, 0, len(stack))
		for _, s := range stack {
			if s != "" {
				parts = append(parts, s)
			}
		}
		heads = append(heads, hd{idx: i, level: level, heading: title, path: strings.Join(parts, " > ")})
	}
	out := make([]pageSection, len(heads))
	for j, h := range heads {
		end := len(lines)
		for k := j + 1; k < len(heads); k++ {
			if heads[k].level <= h.level {
				end = heads[k].idx
				break
			}
		}
		// Preview spans only this section's OWN content — up to the very next
		// heading of any level — so a parent's preview isn't its whole subtree.
		ownEnd := len(lines)
		if j+1 < len(heads) {
			ownEnd = heads[j+1].idx
		}
		out[j] = pageSection{
			Level: h.level, Heading: h.heading, Path: h.path,
			headingLine: h.idx, bodyStart: h.idx + 1, end: end,
			Preview: sectionPreview(lines, h.idx+1, ownEnd),
		}
	}
	return out
}

var mdLinkRE = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
var mdStripper = strings.NewReplacer("**", "", "__", "", "*", "", "`", "", "~~", "")

// sectionPreview builds a short, prose-ish snippet from a section's content lines
// (fence-aware), stripping the loudest markdown so an agent can tell what a
// section is about without reading the body. Clamped to ~140 runes.
func sectionPreview(lines []string, start, end int) string {
	const cap = 140
	var b strings.Builder
	inFence := false
	for i := start; i < end && i < len(lines); i++ {
		ln := strings.TrimSpace(lines[i])
		if strings.HasPrefix(ln, "```") || strings.HasPrefix(ln, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || ln == "" {
			continue
		}
		ln = strings.TrimLeft(ln, "#>-*+| \t")
		ln = mdLinkRE.ReplaceAllString(ln, "$1")
		ln = strings.TrimSpace(mdStripper.Replace(ln))
		if ln == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(ln)
		if len([]rune(b.String())) >= cap {
			break
		}
	}
	r := []rune(b.String())
	if len(r) > cap {
		return strings.TrimRight(string(r[:cap]), " ") + "…"
	}
	return string(r)
}

// matchSection resolves a patch target to a section: exact path, then exact
// heading text, then a path suffix ("Production" matches "Deploy > Production").
// nil when nothing matches.
func matchSection(sections []pageSection, target string) *pageSection {
	t := strings.ToLower(strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(target), "#")))
	for i := range sections {
		if strings.ToLower(sections[i].Path) == t {
			return &sections[i]
		}
	}
	for i := range sections {
		if strings.ToLower(sections[i].Heading) == t {
			return &sections[i]
		}
	}
	for i := range sections {
		if strings.HasSuffix(strings.ToLower(sections[i].Path), "> "+t) {
			return &sections[i]
		}
	}
	return nil
}

func sectionPaths(sections []pageSection) string {
	ps := make([]string, len(sections))
	for i, s := range sections {
		ps[i] = s.Path
	}
	return strings.Join(ps, ", ")
}

// applyPatch returns body with op applied at the section named by target. ops:
// append (after the section body), prepend (right under the heading), replace
// (swap the section body, heading kept), delete (remove heading + body). The
// matched section is returned for the caller's response.
func applyPatch(body, target, op, content string) (string, *pageSection, error) {
	sections := pageOutline(body)
	sec := matchSection(sections, target)
	if sec == nil {
		if len(sections) == 0 {
			return "", nil, fmt.Errorf("page has no headings to target; sections: (none)")
		}
		return "", nil, fmt.Errorf("section %q not found; sections: %s", target, sectionPaths(sections))
	}
	lines := strings.Split(body, "\n")
	add := strings.Split(strings.Trim(content, "\n"), "\n")

	var out []string
	switch op {
	case "append":
		out = append(out, lines[:sec.end]...)
		out = append(out, "")
		out = append(out, add...)
		out = append(out, lines[sec.end:]...)
	case "prepend":
		out = append(out, lines[:sec.bodyStart]...)
		out = append(out, "")
		out = append(out, add...)
		out = append(out, lines[sec.bodyStart:]...)
	case "replace":
		out = append(out, lines[:sec.bodyStart]...)
		out = append(out, "")
		out = append(out, add...)
		out = append(out, "")
		out = append(out, lines[sec.end:]...)
	case "delete":
		out = append(out, lines[:sec.headingLine]...)
		out = append(out, lines[sec.end:]...)
	default:
		return "", nil, fmt.Errorf("unknown operation %q (use append, prepend, replace, or delete)", op)
	}
	// Collapse any run of 3+ blank lines the splice may have introduced down to one.
	return collapseBlankRuns(strings.Join(out, "\n")), sec, nil
}

var blankRunRE = regexp.MustCompile(`\n{3,}`)

func collapseBlankRuns(s string) string {
	return strings.Trim(blankRunRE.ReplaceAllString(s, "\n\n"), "\n") + "\n"
}
