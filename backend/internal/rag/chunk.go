package rag

import (
	"regexp"
	"strings"
)

// Chunk is one retrievable unit: a slice of a page's markdown under a heading
// path, plus the text actually embedded (heading-path-prefixed for context).
type Chunk struct {
	Ord         int
	HeadingPath string // "Deploy > Production"
	Content     string // raw markdown slice (stored + returned + lexically indexed)
	EmbedText   string // contextualised text (page title + heading path + content) — what we embed
}

var headingRE = regexp.MustCompile(`^(#{1,6})\s+(.*\S)\s*$`)

// maxChunkChars caps a section before a forced flush. ~2000 chars ≈ ~500
// tokens — the recursive-split sweet spot. Heading boundaries flush earlier;
// long sections split into multiple chunks sharing a heading path.
const maxChunkChars = 2000

// ChunkMarkdown splits a page body into heading-aware chunks. Each chunk carries
// the heading breadcrumb it lives under; the page title + breadcrumb is folded
// into EmbedText so every embedded chunk is self-contained (contextual
// retrieval). Fenced code blocks are never split and `#` inside a fence is not
// treated as a heading. Callers should StripExcalidrawFences(body) first.
func ChunkMarkdown(pageTitle, body string) []Chunk {
	lines := strings.Split(body, "\n")
	var (
		out      []Chunk
		stack    []string // heading title per level (index 0 = h1)
		buf      []string
		inFence  bool
		fenceTok string
	)

	flush := func() {
		content := strings.TrimSpace(strings.Join(buf, "\n"))
		buf = buf[:0]
		if content == "" {
			return
		}
		parts := make([]string, 0, len(stack))
		for _, s := range stack {
			if s != "" {
				parts = append(parts, s)
			}
		}
		hp := strings.Join(parts, " > ")
		ctx := pageTitle
		if hp != "" {
			ctx += " — " + hp
		}
		out = append(out, Chunk{
			Ord:         len(out),
			HeadingPath: hp,
			Content:     content,
			EmbedText:   ctx + "\n\n" + content,
		})
	}

	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)

		// Track fenced code so heading and length rules ignore fence interiors.
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			tok := trimmed[:3]
			switch {
			case !inFence:
				inFence, fenceTok = true, tok
			case tok == fenceTok:
				inFence = false
			}
			buf = append(buf, ln)
			continue
		}

		if !inFence {
			if m := headingRE.FindStringSubmatch(ln); m != nil {
				flush() // close the current section before starting a new heading
				level := len(m[1])
				title := m[2]
				if level > len(stack) {
					for len(stack) < level-1 {
						stack = append(stack, "")
					}
					stack = append(stack, title)
				} else {
					stack = append(stack[:level-1], title)
				}
				continue
			}
		}

		buf = append(buf, ln)
		if !inFence && bufLen(buf) >= maxChunkChars {
			flush()
		}
	}
	flush()
	return out
}

func bufLen(buf []string) int {
	n := 0
	for _, s := range buf {
		n += len(s) + 1
	}
	return n
}

// excalidrawFenceRE matches a complete ```excalidraw fenced block. Recovered
// from the retired SQLite-era tela_strip_excalidraw UDF — page bodies carry
// ```excalidraw\n{json}\n``` fences whose JSON must not pollute the embedded /
// lexically-indexed text. Anatomy: literal ```excalidraw info string (optional
// space-separated metadata), a newline, lazy multi-line body, optional newline,
// closing fence.
var excalidrawFenceRE = regexp.MustCompile("(?s)```excalidraw(?:[ \\t]+[^\\n]*)?\\n.*?\\n?```")

// StripExcalidrawFences removes every ```excalidraw fenced block from src.
func StripExcalidrawFences(src string) string {
	return excalidrawFenceRE.ReplaceAllString(src, "")
}
