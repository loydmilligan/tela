import { $prose } from '@milkdown/kit/utils'
import { Plugin, PluginKey } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'

// M19 — table upgrades, folded into the stock GFM table (no new block type, no
// new syntax, perfect round-trip). Everything is derived from cell content or
// is a reader-side affordance:
//
//   • Glyph cells — a cell whose entire text is `check`/`cross`/`dash` (or
//     ✓/✗/–, yes/no) renders as a themed, semantically-coloured icon
//     (check=green, cross=red, dash=muted). Click into the cell to edit the
//     keyword (the icon reveals the text on focus). This is the comparison-
//     matrix ✓/✗ grid, absorbed into the table.
//   • Sticky first column — the row-label column pins while you scroll a wide
//     table horizontally (pure CSS; invisible when there's nothing to scroll).
//   • Sort + filter — in read-only / reader / share / PDF views, a genuinely
//     large table (≥8 rows) gets clickable sort headers + a filter box. Reader-
//     only so it never fights editing; small tables stay clean.
//
// Glyph cells are ProseMirror node decorations (both edit + read modes, CSS does
// the visual). Sort/filter is a pure DOM enhancement applied to the rendered
// read-only table via the plugin's view() — exported standalone so the Storybook
// story exercises the exact same code path.

const tableKey = new PluginKey('tela-table-enhance')

// Exact-match glyph keywords (case-insensitive). Symbols pass through unchanged.
const GLYPHS: Record<string, 'check' | 'cross' | 'dash'> = {
  check: 'check',
  '✓': 'check',
  '✔': 'check',
  yes: 'check',
  cross: 'cross',
  '✗': 'cross',
  '✕': 'cross',
  '×': 'cross',
  no: 'cross',
  dash: 'dash',
  '–': 'dash',
  '—': 'dash',
  '-': 'dash',
  'n/a': 'dash',
}

function glyphFor(text: string): 'check' | 'cross' | 'dash' | null {
  const t = text.trim()
  if (!t) return null
  return GLYPHS[t.toLowerCase()] ?? null
}

function buildDecorations(doc: ProseNode): DecorationSet {
  const decos: Decoration[] = []
  doc.descendants((node, pos) => {
    if (node.type.name !== 'table') return true
    node.forEach((row, rowOffset) => {
      if (row.type.name !== 'table_row') return
      const rowPos = pos + 1 + rowOffset
      row.forEach((cell, cellOffset) => {
        const cellPos = rowPos + 1 + cellOffset
        const g = glyphFor(cell.textContent)
        if (g) {
          decos.push(
            Decoration.node(cellPos, cellPos + cell.nodeSize, {
              class: `tela-cell-glyph tela-cell-glyph-${g}`,
            }),
          )
        }
      })
    })
    return false // handled the whole table; don't descend into its cells
  })
  return DecorationSet.create(doc, decos)
}

// Numeric-aware comparison so "12" sorts after "2", and currency/percent values
// sort by their number. Falls back to locale string compare.
function compareCells(a: string, b: string): number {
  const na = parseFloat(a.replace(/[^0-9.+-]/g, ''))
  const nb = parseFloat(b.replace(/[^0-9.+-]/g, ''))
  const aNum = a.trim() !== '' && !Number.isNaN(na) && /\d/.test(a)
  const bNum = b.trim() !== '' && !Number.isNaN(nb) && /\d/.test(b)
  if (aNum && bNum) return na - nb
  return a.trim().localeCompare(b.trim(), undefined, { numeric: true })
}

function cellText(row: HTMLTableRowElement, i: number): string {
  return row.cells[i]?.textContent?.trim() ?? ''
}

// Pure DOM enhancement: clickable sort headers + (for larger tables) a filter
// box. Idempotent. Used by the read-only plugin view AND the Storybook story.
export function enhanceReadonlyTable(table: HTMLTableElement): void {
  if (table.dataset.telaEnhanced) return
  const thead = table.tHead
  const tbody = table.tBodies[0]
  const headRow = thead?.rows[0]
  if (!headRow || !tbody) return
  const headCells = Array.from(headRow.cells)
  if (headCells.length === 0) return
  // Only earn the sort/filter chrome on genuinely large tables (mira's ~8-row
  // threshold) — small comparison tables stay clean.
  if (tbody.rows.length < 8) return
  table.dataset.telaEnhanced = '1'

  headCells.forEach((th, i) => {
    th.classList.add('tela-th-sortable')
    let dir = 0
    th.addEventListener('click', () => {
      dir = dir === 1 ? -1 : 1
      headCells.forEach((h) => h.removeAttribute('data-sort'))
      th.dataset.sort = dir === 1 ? 'asc' : 'desc'
      const rows = Array.from(tbody.rows)
      rows.sort((ra, rb) => compareCells(cellText(ra, i), cellText(rb, i)) * dir)
      rows.forEach((r) => tbody.appendChild(r))
    })
  })

  // Filter box (we're already past the ≥8-row gate).
  const wrap = table.closest('.tableWrapper') ?? table
  const bar = document.createElement('div')
  bar.className = 'tela-table-filter'
  const input = document.createElement('input')
  input.type = 'text'
  input.placeholder = 'Filter rows…'
  input.setAttribute('aria-label', 'Filter table rows')
  input.addEventListener('input', () => {
    const q = input.value.trim().toLowerCase()
    for (const row of Array.from(tbody.rows)) {
      const hit = !q || (row.textContent ?? '').toLowerCase().includes(q)
      row.style.display = hit ? '' : 'none'
    }
  })
  bar.appendChild(input)
  wrap.parentElement?.insertBefore(bar, wrap)
}

export const tableEnhancePlugin = $prose(() => {
  return new Plugin({
    key: tableKey,
    props: {
      decorations(state) {
        return buildDecorations(state.doc)
      },
    },
    view(editorView) {
      const run = () => {
        if (editorView.editable) return
        // GFM content tables only — exclude block-internal tables like the
        // calendar month grid (`.tela-calendar-table`), which is a <table> too
        // but must never get sort/filter chrome. (In the reader, GFM tables are
        // NOT wrapped in `.tableWrapper`, so we can't filter on that.)
        editorView.dom
          .querySelectorAll('table:not(.tela-calendar-table)')
          .forEach((t) => enhanceReadonlyTable(t as HTMLTableElement))
      }
      run()
      return { update: run }
    },
  })
})
