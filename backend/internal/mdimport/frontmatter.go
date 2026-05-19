package mdimport

import (
	"regexp"
	"strings"
)

// frontmatterRE matches a leading YAML-frontmatter block: a `---` line at the
// very start of the document, content, then a closing `---` line. We accept
// both LF and CRLF line endings so files written on Windows still round-trip.
var frontmatterRE = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n?`)

// titleRE pulls a single `title: …` line out of a frontmatter block. Multiline
// scoped to the captured block — no nested-key support, no list values.
var titleRE = regexp.MustCompile(`(?m)^title:\s*(.*?)\s*$`)

// h1RE finds the first ATX H1 (`# Heading`) anywhere in the body.
var h1RE = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)

// StripFrontmatter detects a leading YAML frontmatter block and returns the
// body with the block removed plus the frontmatter title (empty if absent or
// no `title:` field). When no frontmatter is detected the original content is
// returned unchanged. The parser is intentionally minimal — only `title:`
// matters for v0; everything else in the frontmatter is dropped silently.
func StripFrontmatter(content string) (body, title string) {
	m := frontmatterRE.FindStringIndex(content)
	if m == nil {
		return content, ""
	}
	fmText := content[m[0]:m[1]]
	body = content[m[1]:]
	if mm := frontmatterRE.FindStringSubmatch(fmText); mm != nil {
		title = parseFrontmatterTitle(mm[1])
	}
	return body, title
}

// parseFrontmatterTitle extracts the `title:` value from a frontmatter block.
// Surrounding single or double quotes are stripped — bare YAML scalars,
// `title: "Foo"`, and `title: 'Foo'` all return `Foo`.
func parseFrontmatterTitle(block string) string {
	m := titleRE.FindStringSubmatch(block)
	if m == nil {
		return ""
	}
	t := strings.TrimSpace(m[1])
	if len(t) >= 2 {
		if (t[0] == '"' && t[len(t)-1] == '"') || (t[0] == '\'' && t[len(t)-1] == '\'') {
			t = t[1 : len(t)-1]
		}
	}
	return strings.TrimSpace(t)
}

// FirstH1Title returns the text of the first ATX H1 heading in body, or "" if
// there is none. Caller is responsible for first stripping frontmatter — the
// regex matches anywhere in the input, so passing raw frontmatter would still
// look inside it.
func FirstH1Title(body string) string {
	m := h1RE.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}
