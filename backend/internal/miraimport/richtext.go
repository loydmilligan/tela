package miraimport

import "strings"

// richText is one segment inside any mira rich_text array. We decode only the
// fields the converter actually uses; mira's closed schema rejects unknown
// fields on POST but tolerates them on read, so leaving extras absent here is
// safe.
type richText struct {
	Type string `json:"type"`
	Text struct {
		Content string `json:"content"`
		Link    *struct {
			URL string `json:"url"`
		} `json:"link,omitempty"`
	} `json:"text"`
	Annotations struct {
		Bold          bool `json:"bold"`
		Italic        bool `json:"italic"`
		Strikethrough bool `json:"strikethrough"`
		Underline     bool `json:"underline"`
		Code          bool `json:"code"`
	} `json:"annotations"`
	Href string `json:"href,omitempty"`
}

// renderRichText concatenates a rich_text array into markdown. Annotation
// stacking order (outer → inner): href > bold > italic > code > strikethrough.
// code annotations short-circuit nested marks because markdown inline code is
// literal — no marks render inside backticks.
func renderRichText(rts []richText) string {
	if len(rts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, rt := range rts {
		b.WriteString(renderSegment(rt))
	}
	return b.String()
}

// renderRichTextPlain flattens a rich_text array into plain text, dropping
// every annotation. Used for heading-derived titles and the callout-as-alert
// label line where markdown marks would just be noise.
func renderRichTextPlain(rts []richText) string {
	var b strings.Builder
	for _, rt := range rts {
		b.WriteString(rt.Text.Content)
	}
	return b.String()
}

func renderSegment(rt richText) string {
	content := rt.Text.Content

	// Code annotation is literal — no nested marks, no escaping.
	if rt.Annotations.Code {
		out := "`" + content + "`"
		return wrapLink(out, linkURL(rt))
	}

	// Plain-text path: escape markdown specials then layer marks.
	out := escapeMarkdown(content)
	if rt.Annotations.Strikethrough {
		out = "~~" + out + "~~"
	}
	if rt.Annotations.Italic {
		out = "*" + out + "*"
	}
	if rt.Annotations.Bold {
		out = "**" + out + "**"
	}
	// Underline / status / style / color → dropped (no markdown equivalent).
	return wrapLink(out, linkURL(rt))
}

// linkURL returns the effective link URL for a rich_text segment. mira spec:
// `text.link.url` wins over the top-level `href` echo when both are present.
func linkURL(rt richText) string {
	if rt.Text.Link != nil && rt.Text.Link.URL != "" {
		return rt.Text.Link.URL
	}
	return rt.Href
}

func wrapLink(text, url string) string {
	if url == "" {
		return text
	}
	return "[" + text + "](" + url + ")"
}

// markdown special characters that need backslash-escape inside plain text
// runs. Intentionally narrow: covers the inline-affecting set without
// over-escaping (which would render as visible backslashes in legacy renderers).
var markdownEscaper = strings.NewReplacer(
	`\`, `\\`,
	"`", "\\`",
	`*`, `\*`,
	`_`, `\_`,
	`[`, `\[`,
	`]`, `\]`,
	`<`, `\<`,
	`>`, `\>`,
	`~`, `\~`,
)

func escapeMarkdown(s string) string {
	return markdownEscaper.Replace(s)
}
