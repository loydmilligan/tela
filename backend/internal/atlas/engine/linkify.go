package engine

import (
	"strconv"
	"strings"

	"github.com/zcag/tela/backend/internal/atlas/core"
	"github.com/zcag/tela/backend/internal/atlas/source"
)

// linkifyCitations rewrites resolvable `path:line` (or `path:l1-l2`) citations in
// a page body into clickable markdown links, using source.CiteURL for the target.
// It is publish-time only — the audit still operates on the raw `path:line` form.
//
// Rules:
//   - Only citations whose path resolves to a real corpus file (same suffix match
//     the audit uses) are linked; unresolvable ones (the BadCitations) are left
//     untouched as plain text.
//   - Matches inside fenced ``` code blocks are skipped (don't linkify code/log
//     lines), as are citations already wrapped in a markdown link.
//   - When CiteURL returns "" (unsupported host / non-issue jira path) the
//     citation is left as plain text.
func linkifyCitations(src core.Source, files []core.File, body string) string {
	lines := strings.Split(body, "\n")
	inFence := false
	for i, line := range lines {
		if isFence(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i] = linkifyLine(src, files, line)
	}
	return strings.Join(lines, "\n")
}

// isFence reports whether a line opens or closes a ``` / ~~~ code fence.
func isFence(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// linkifyLine replaces resolvable citations on a single (non-fenced) line with a
// single clean clickable link whose text is a code span: [`<cite>`](url). Any
// backticks and/or square brackets immediately wrapping the citation are consumed
// so the output has no stray delimiters — markdown would otherwise render
// `[cite](url)` as literal code, or [[cite](url)] double-bracketed.
func linkifyLine(src core.Source, files []core.File, line string) string {
	ms := citeRe.FindAllStringSubmatchIndex(line, -1)
	if ms == nil {
		return line
	}
	var b strings.Builder
	last := 0
	for _, m := range ms {
		start, end := m[0], m[1]
		// already inside a markdown link target/label → leave it.
		if alreadyLinked(line, start, end) {
			continue
		}
		cite := line[start:end]
		path := line[m[2]:m[3]]
		l1, _ := strconv.Atoi(line[m[4]:m[5]])
		l2 := l1
		if m[6] >= 0 {
			l2, _ = strconv.Atoi(line[m[6]:m[7]])
		}
		realPath, _, ok := resolveCite(files, path)
		if !ok {
			continue // unresolvable: a BadCitation, leave as-is
		}
		url := source.CiteURL(src, realPath, l1, l2)
		if url == "" {
			continue // no clickable target for this source/host
		}
		// consume any wrapping delimiters: backticks (Sources list / inline code
		// spans) and/or [ ] (the inline "Sources: [path:line]" form).
		ws, we := absorbDelims(line, start, end)
		if ws < last { // overlaps an already-emitted span
			continue
		}
		b.WriteString(line[last:ws])
		b.WriteString("[`")
		b.WriteString(cite)
		b.WriteString("`](")
		b.WriteString(url)
		b.WriteString(")")
		last = we
	}
	b.WriteString(line[last:])
	return b.String()
}

// absorbDelims widens [start,end) to swallow a single pair of backticks and/or a
// single pair of square brackets immediately wrapping the citation, in either
// nesting order (`[cite]`, [`cite`], `cite`, [cite]). Returns the widened bounds.
func absorbDelims(line string, start, end int) (int, int) {
	for {
		grew := false
		if start >= 1 && end < len(line) && line[start-1] == '`' && line[end] == '`' {
			start, end, grew = start-1, end+1, true
		}
		if start >= 1 && end < len(line) && line[start-1] == '[' && line[end] == ']' {
			start, end, grew = start-1, end+1, true
		}
		if !grew {
			return start, end
		}
	}
}

// alreadyLinked reports whether the match at [start,end) is already part of a
// markdown link — either its label (preceded by '[', followed by "](") or its
// target (immediately preceded by "](").
func alreadyLinked(line string, start, end int) bool {
	if start >= 1 && line[start-1] == '[' && strings.HasPrefix(line[end:], "](") {
		return true
	}
	if start >= 2 && line[start-2] == ']' && line[start-1] == '(' {
		return true
	}
	return false
}
