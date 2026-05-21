// Package miraimport converts mira (mira.cagdas.io) Notion-style block JSON
// payloads into tela markdown.
//
// Mira is a public render service: POST a closed-schema block document
// (29 block types, 5 MB / 200-block / 2000-rich-text caps) and it serves an
// HTML page at /p/<slug>. Tela imports these pages so the content outlives
// the public link.
//
// Tiered fidelity (M18 design):
//   - Tier-1 blocks (this file): the 14 types that map cleanly to markdown —
//     headings, paragraph, lists, code, quote, divider, image, table,
//     callout, toggle, mermaid. Rendered faithfully.
//   - Tier-2 blocks (M18.A.2, follow-up): 15 visual types
//     (chart, kanban, timeline, …) rendered as best-effort markdown
//     placeholders.
//   - Unknown / forward-compat: any unrecognized type emits a
//     `> ⚠️ Unsupported mira block: <type>` blockquote stub. Schema additions
//     don't break import; they just render as a visible TODO.
//
// Public surface: Convert([]byte) (title, body string, err error).
// Title comes from the first heading_1 block (flattened rich_text); falls back
// to "(untitled mira page)" when absent. Empty `blocks: []` returns the
// fallback title and an empty body without error.
package miraimport

import (
	"encoding/json"
	"fmt"
	"strings"
)

// fallbackTitle is the title used when no heading_1 block exists in the
// payload (or when the document has zero blocks).
const fallbackTitle = "(untitled mira page)"

// page is the top-level mira payload envelope. Optional fields
// (persistent, password, theme_variant) are ignored on import.
type page struct {
	Template string  `json:"template"`
	Blocks   []block `json:"blocks"`
}

// block is one mira block. mira encodes the body under a key matching the
// type — e.g. {"type":"paragraph","paragraph":{...}}. We keep the entire
// raw object so per-type decoders can pick out their body key.
type block struct {
	Type string
	Raw  json.RawMessage
}

func (b *block) UnmarshalJSON(data []byte) error {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	b.Type = head.Type
	b.Raw = append(b.Raw[:0], data...)
	return nil
}

// Convert decodes a mira page JSON payload and returns the page title plus
// rendered markdown body.
func Convert(payload []byte) (title, body string, err error) {
	var p page
	if err := json.Unmarshal(payload, &p); err != nil {
		return "", "", fmt.Errorf("decode mira payload: %w", err)
	}
	if len(p.Blocks) == 0 {
		return fallbackTitle, "", nil
	}
	return extractTitle(p.Blocks), renderBlocks(p.Blocks), nil
}

// extractTitle pulls the first heading_1's text. If absent (or its rich_text
// is empty / whitespace-only), fall back to the constant.
func extractTitle(blocks []block) string {
	for _, b := range blocks {
		if b.Type != "heading_1" {
			continue
		}
		var body struct {
			RichText []richText `json:"rich_text"`
		}
		if err := decodeBody(b, "heading_1", &body); err != nil {
			continue
		}
		t := strings.TrimSpace(renderRichTextPlain(body.RichText))
		if t != "" {
			return t
		}
	}
	return fallbackTitle
}

// renderBlocks renders a list of top-level (or nested) blocks and joins them
// with the standard markdown paragraph separator (blank line). Empty
// renderings are skipped so we don't generate triple-newlines.
func renderBlocks(blocks []block) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		s := renderBlock(b)
		if s == "" {
			continue
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n\n")
}

// renderBlock dispatches one block to its type-specific renderer. Unknown
// types fall through to renderUnknown — the same forward-compat stub used by
// M18.A.2 for not-yet-implemented Tier-2 types.
func renderBlock(b block) string {
	switch b.Type {
	case "heading_1":
		return renderHeading(b, "#")
	case "heading_2":
		return renderHeading(b, "##")
	case "heading_3":
		return renderHeading(b, "###")
	case "paragraph":
		return renderParagraph(b)
	case "bulleted_list_item":
		return renderListItem(b, false)
	case "numbered_list_item":
		return renderListItem(b, true)
	case "code":
		return renderCode(b)
	case "quote":
		return renderQuote(b)
	case "divider":
		return "---"
	case "image":
		return renderImage(b)
	case "table":
		return renderTable(b)
	case "callout":
		return renderCallout(b)
	case "toggle":
		return renderToggle(b)
	case "mermaid":
		return renderMermaid(b)
	}
	return renderUnknown(b.Type)
}

// renderUnknown emits the forward-compat stub for any block.type we don't
// know how to render. Used both for Tier-2 blocks (until M18.A.2 lands their
// converters) and for genuine unknowns (future mira schema additions).
func renderUnknown(t string) string {
	return "> ⚠️ Unsupported mira block: " + t
}

// decodeBody pulls the body sub-object keyed by `key` from a block's raw
// envelope and unmarshals it into out. Returns an error when the body is
// missing or malformed — callers either skip the block or surface a stub.
func decodeBody(b block, key string, out interface{}) error {
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(b.Raw, &wrap); err != nil {
		return err
	}
	body, ok := wrap[key]
	if !ok {
		return fmt.Errorf("missing %q body", key)
	}
	return json.Unmarshal(body, out)
}

// ----- block renderers -----

func renderHeading(b block, hashes string) string {
	var body struct {
		RichText []richText `json:"rich_text"`
	}
	if err := decodeBody(b, b.Type, &body); err != nil {
		return ""
	}
	text := renderRichText(body.RichText)
	if text == "" {
		return ""
	}
	return hashes + " " + text
}

func renderParagraph(b block) string {
	var body struct {
		RichText []richText `json:"rich_text"`
		Body     string     `json:"body,omitempty"`
		Editable bool       `json:"editable,omitempty"`
	}
	if err := decodeBody(b, "paragraph", &body); err != nil {
		return ""
	}
	if body.Editable && body.Body != "" {
		return escapeMarkdown(body.Body)
	}
	return renderRichText(body.RichText)
}

// renderListItem renders one list item plus any nested children. Markdown
// nested lists use 2-space indents per depth level; the top call indents at
// level 0 and children recurse at level 1+.
func renderListItem(b block, ordered bool) string {
	return renderListItemAt(b, ordered, 0)
}

func renderListItemAt(b block, ordered bool, depth int) string {
	var body struct {
		RichText []richText `json:"rich_text"`
		Children []block    `json:"children,omitempty"`
	}
	bodyKey := "bulleted_list_item"
	if ordered {
		bodyKey = "numbered_list_item"
	}
	if err := decodeBody(b, bodyKey, &body); err != nil {
		return ""
	}
	indent := strings.Repeat("  ", depth)
	marker := "-"
	if ordered {
		marker = "1."
	}
	line := indent + marker + " " + renderRichText(body.RichText)

	if len(body.Children) == 0 {
		return line
	}
	parts := []string{line}
	for _, child := range body.Children {
		switch child.Type {
		case "bulleted_list_item":
			parts = append(parts, renderListItemAt(child, false, depth+1))
		case "numbered_list_item":
			parts = append(parts, renderListItemAt(child, true, depth+1))
		default:
			// Non-list children: render and indent so they hang under the item.
			rendered := renderBlock(child)
			if rendered == "" {
				continue
			}
			parts = append(parts, indentLines(rendered, strings.Repeat("  ", depth+1)))
		}
	}
	return strings.Join(parts, "\n")
}

func renderCode(b block) string {
	var body struct {
		RichText []richText `json:"rich_text"`
		Language string     `json:"language,omitempty"`
		Caption  []richText `json:"caption,omitempty"`
	}
	if err := decodeBody(b, "code", &body); err != nil {
		return ""
	}
	// Code body is literal — marks are suppressed per mira spec; we just
	// concatenate plain content.
	content := renderRichTextPlain(body.RichText)
	lang := strings.TrimSpace(body.Language)
	if lang == "" || strings.EqualFold(lang, "plain text") {
		lang = ""
	}
	out := "```" + lang + "\n" + content
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += "```"
	if caption := strings.TrimSpace(renderRichText(body.Caption)); caption != "" {
		out += "\n\n" + caption
	}
	return out
}

func renderQuote(b block) string {
	var body struct {
		RichText []richText `json:"rich_text"`
		Children []block    `json:"children,omitempty"`
	}
	if err := decodeBody(b, "quote", &body); err != nil {
		return ""
	}
	lines := []string{renderRichText(body.RichText)}
	if len(body.Children) > 0 {
		nested := renderBlocks(body.Children)
		if nested != "" {
			lines = append(lines, "", nested)
		}
	}
	joined := strings.Join(lines, "\n")
	return prefixLines(joined, "> ")
}

func renderImage(b block) string {
	var body struct {
		Type     string `json:"type"`
		External *struct {
			URL string `json:"url"`
		} `json:"external,omitempty"`
		File *struct {
			URL string `json:"url"`
		} `json:"file,omitempty"`
		Caption []richText `json:"caption,omitempty"`
	}
	if err := decodeBody(b, "image", &body); err != nil {
		return ""
	}
	url := ""
	switch body.Type {
	case "external":
		if body.External != nil {
			url = body.External.URL
		}
	case "file":
		if body.File != nil {
			url = body.File.URL
		}
	}
	if url == "" {
		return ""
	}
	caption := strings.TrimSpace(renderRichText(body.Caption))
	alt := strings.TrimSpace(renderRichTextPlain(body.Caption))
	// alt text inside ![…] needs `]` escaped to avoid breaking the syntax.
	alt = strings.ReplaceAll(alt, "]", `\]`)
	img := "![" + alt + "](" + url + ")"
	if caption != "" {
		return img + "\n\n" + caption
	}
	return img
}

func renderTable(b block) string {
	var body struct {
		TableWidth      int     `json:"table_width"`
		HasColumnHeader bool    `json:"has_column_header"`
		HasRowHeader    bool    `json:"has_row_header"`
		Children        []block `json:"children"`
	}
	if err := decodeBody(b, "table", &body); err != nil {
		return ""
	}
	width := body.TableWidth
	if width <= 0 {
		return ""
	}
	rows := make([][]string, 0, len(body.Children))
	for _, child := range body.Children {
		if child.Type != "table_row" {
			continue
		}
		var rb struct {
			Cells [][]richText `json:"cells"`
		}
		if err := decodeBody(child, "table_row", &rb); err != nil {
			continue
		}
		row := make([]string, width)
		for i := 0; i < width; i++ {
			if i < len(rb.Cells) {
				// Markdown table cells can't contain raw pipes or newlines.
				cell := renderRichText(rb.Cells[i])
				cell = strings.ReplaceAll(cell, "|", `\|`)
				cell = strings.ReplaceAll(cell, "\n", " ")
				row[i] = cell
			} else {
				row[i] = ""
			}
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return ""
	}
	// GFM tables require a header row. When mira's table has no
	// has_column_header, synthesize an empty header so the separator stays
	// valid markdown (rendered cells just look slightly off, never broken).
	var headerRow []string
	bodyRows := rows
	if body.HasColumnHeader {
		headerRow = rows[0]
		bodyRows = rows[1:]
	} else {
		headerRow = make([]string, width)
	}

	var b2 strings.Builder
	writeRow(&b2, headerRow)
	b2.WriteString("\n")
	writeSeparator(&b2, width)
	for _, r := range bodyRows {
		b2.WriteString("\n")
		writeRow(&b2, r)
	}
	return b2.String()
}

func writeRow(w *strings.Builder, cells []string) {
	w.WriteString("|")
	for _, c := range cells {
		w.WriteString(" ")
		if c == "" {
			w.WriteString(" ")
		} else {
			w.WriteString(c)
			w.WriteString(" ")
		}
		w.WriteString("|")
	}
}

func writeSeparator(w *strings.Builder, width int) {
	w.WriteString("|")
	for i := 0; i < width; i++ {
		w.WriteString(" --- |")
	}
}

// calloutVariantFromEmoji maps the mira callout icon emoji to the closest of
// tela's 5 GitHub-flavor alert variants. The mapping is deliberately
// permissive — unknown emojis fall through to NOTE so an imported page never
// loses its callout structure entirely.
//
// Mapping rationale: severity escalation note → tip → important → warning →
// caution. info/idea emojis land on note/tip, success on tip, exclamation on
// important, warning sign on warning, stop/error on caution.
func calloutVariantFromEmoji(emoji string) string {
	switch emoji {
	case "ℹ️", "📝", "📄", "🗒️", "📋":
		return "NOTE"
	case "💡", "✅", "🎉", "🌟", "✨":
		return "TIP"
	case "❗", "‼️", "📌", "📢", "🔔":
		return "IMPORTANT"
	case "⚠️":
		return "WARNING"
	case "🛑", "🚫", "❌", "🔥", "💥":
		return "CAUTION"
	}
	return "NOTE"
}

func renderCallout(b block) string {
	var body struct {
		Icon struct {
			Type  string `json:"type"`
			Emoji string `json:"emoji"`
		} `json:"icon"`
		RichText []richText `json:"rich_text"`
		Children []block    `json:"children,omitempty"`
	}
	if err := decodeBody(b, "callout", &body); err != nil {
		return ""
	}
	variant := calloutVariantFromEmoji(body.Icon.Emoji)
	text := renderRichText(body.RichText)

	lines := []string{"[!" + variant + "]"}
	if text != "" {
		lines = append(lines, text)
	}
	if len(body.Children) > 0 {
		nested := renderBlocks(body.Children)
		if nested != "" {
			if text != "" {
				lines = append(lines, "")
			}
			lines = append(lines, nested)
		}
	}
	return prefixLines(strings.Join(lines, "\n"), "> ")
}

func renderToggle(b block) string {
	var body struct {
		RichText []richText `json:"rich_text"`
		Children []block    `json:"children,omitempty"`
	}
	if err := decodeBody(b, "toggle", &body); err != nil {
		return ""
	}
	// Summary text in <summary> is HTML — strip markdown marks (the milkdown
	// collapsibles plugin reads textContent only) and HTML-escape the result.
	summary := htmlEscape(renderRichTextPlain(body.RichText))
	if summary == "" {
		summary = " "
	}
	innerBody := renderBlocks(body.Children)
	if innerBody == "" {
		return "<details><summary>" + summary + "</summary>\n\n</details>"
	}
	return "<details><summary>" + summary + "</summary>\n\n" + innerBody + "\n\n</details>"
}

func renderMermaid(b block) string {
	var body struct {
		Source string `json:"source"`
	}
	if err := decodeBody(b, "mermaid", &body); err != nil {
		return ""
	}
	src := strings.TrimRight(body.Source, "\n")
	if src == "" {
		return ""
	}
	return "```mermaid\n" + src + "\n```"
}

// ----- string helpers -----

// indentLines prepends prefix to every line in s. Used to hang nested
// non-list content beneath a list item.
func indentLines(s, prefix string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// prefixLines is like indentLines but used for blockquote rendering: every
// line gets the marker, including blank ones (so quote continuations stay
// inside the same block-level quote).
func prefixLines(s, prefix string) string {
	if s == "" {
		return strings.TrimRight(prefix, " ")
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = strings.TrimRight(prefix, " ")
		} else {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}

var htmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
)

func htmlEscape(s string) string {
	return htmlEscaper.Replace(s)
}

