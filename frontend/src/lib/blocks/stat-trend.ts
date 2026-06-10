// Stat-grid trend/accent classification, shared by the editor block
// (components/app/milkdown-stat-grid.ts decorations + nodeView) and the
// read-only view renderer (components/view/MarkdownView.tsx) so a tile's
// accent rail and trend-line colouring agree across surfaces.

// Tile accent inferred from the rendered value text. `↑`/`▲`/`+` ⇒ positive,
// `↓`/`▼`/`−`/`-` ⇒ negative, otherwise default (no tint). Keeps the markdown
// clean — the author writes a natural value line, the accent follows the glyph.
export function accentForValue(
  text: string,
): 'positive' | 'negative' | 'default' {
  if (/[↑▲]|(?:^|\s)\+\d/.test(text)) return 'positive'
  if (/[↓▼−]|(?:^|\s)-\d/.test(text)) return 'negative'
  return 'default'
}

// Class for a non-first tile paragraph: a line led by a trend glyph (↑/↓/→ or
// +/-N) becomes a coloured trend row, everything else a muted description.
// (The first paragraph is always the value — `tela-stat-figure`.)
export function statLineClass(text: string): string {
  const t = text.trim()
  if (/^[↑▲]|^\+\d/.test(t)) return 'tela-stat-trend tela-stat-trend-up'
  if (/^[↓▼]|^-\d/.test(t)) return 'tela-stat-trend tela-stat-trend-down'
  if (t.startsWith('→')) return 'tela-stat-trend tela-stat-trend-flat'
  return 'tela-stat-desc'
}
