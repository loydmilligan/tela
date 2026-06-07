package mdimport

import (
	"path"
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

// TitleFor resolves a page title from the locked import/sync precedence:
// frontmatter title → first H1 in the body → filename stem → "Untitled".
// body must already be frontmatter-stripped. Shared by the bulk importer
// (Pass 2) and the sync ingress so the two never drift.
func TitleFor(fmTitle, body, filename string) string {
	t := strings.TrimSpace(fmTitle)
	if t == "" {
		t = FirstH1Title(body)
	}
	if t == "" {
		t = filenameStem(filename)
	}
	t = strings.TrimSpace(t)
	if t == "" {
		return "Untitled"
	}
	return t
}

// filenameStem returns the basename without a trailing .md/.markdown extension
// (case-insensitive), preserving the human casing/spacing of the name.
func filenameStem(filename string) string {
	base := path.Base(filename)
	lower := strings.ToLower(base)
	switch {
	case strings.HasSuffix(lower, ".markdown"):
		return base[:len(base)-len(".markdown")]
	case strings.HasSuffix(lower, ".md"):
		return base[:len(base)-len(".md")]
	default:
		return base
	}
}
