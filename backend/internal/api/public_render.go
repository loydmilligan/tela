package api

import (
	"bytes"
	"html/template"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// mdRenderer renders canonical page markdown to HTML for the crawler-visible
// body of public OG documents (HandlePublicReaderOG). GFM for tables/autolinks/
// strikethrough; raw HTML is left DISABLED (no WithUnsafe) so a public page's
// stored markdown can't inject script/style into the bot document. tela's custom
// block directives (:::tabs, [!NOTE], mermaid, …) aren't renderer plugins here —
// they degrade to their inner text/blockquote, which is exactly what a crawler
// needs (the prose is indexable); the human SPA still renders them richly.
var mdRenderer = goldmark.New(goldmark.WithExtensions(extension.GFM))

// renderPublicBodyHTML turns page markdown into sanitized body HTML. On any
// render error it returns empty — the OG doc still carries title/description/
// schema, so a failure degrades to the prior meta-only behaviour, never a 500.
func renderPublicBodyHTML(md string) template.HTML {
	if md == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := mdRenderer.Convert([]byte(md), &buf); err != nil {
		return ""
	}
	return template.HTML(buf.String()) //nolint:gosec // raw HTML disabled above; goldmark escapes it
}
