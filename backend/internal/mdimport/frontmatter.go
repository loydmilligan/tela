package mdimport

import (
	"regexp"
	"strings"
)

// The frontmatter codec (Decode/Encode/FilterReserved) lives in package pagemd —
// the shared, pure round-trip kernel. This file keeps only the import-pipeline's
// title fallback, which is import-specific (not part of the codec).

// h1RE finds the first ATX H1 (`# Heading`) anywhere in the body.
var h1RE = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)

// FirstH1Title returns the text of the first ATX H1 heading in body, or "" if
// there is none. Caller is responsible for first stripping frontmatter.
func FirstH1Title(body string) string {
	m := h1RE.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}
