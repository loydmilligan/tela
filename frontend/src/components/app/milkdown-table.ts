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
//   • Featured column — a header cell whose text is fully ==highlighted==
//     marks that whole column as featured (a soft accent wash). Explicit and
//     intentional (a plain bold header won't trigger it), and it round-trips
//     as the `==…==` highlight mark.
//   • Sticky first column — the row-label column pins while you scroll a wide
//     table horizontally (pure CSS; invisible when there's nothing to scroll).
//   • Sort + filter — in read-only / reader / share / PDF views, a column-
//     headed table gets clickable sort headers, and tables with enough rows get
//     a filter box. Reader-only so it never fights editing.
//
// Glyph + featured are ProseMirror node decorations (both edit and read modes,
// CSS does the visual). Sort/filter is a pure DOM enhancement applied to the
// rendered read-only table via the plugin's view() — exported standalone so the
// Storybook story exercises the exact same code path.

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

// A header cell counts as "featured" when it has text and every text node in it
// carries the highlight mark (i.e. the author wrote `==Label==`).
function cellFullyHighlighted(cell: ProseNode): boolean {
  let hasText = false
  let allMarked = true
  cell.descendants((n) => {
    if (n.isText && n.text && n.text.trim()) {
      hasText = true
      if (!n.marks.some((m) => m.type.name === 'highlight')) allMarked = false
    }
  })
  return hasText && allMarked
}

function buildDecorations(doc: ProseNode): DecorationSet {
  const decos: Decoration[] = []
  doc.descendants((node, pos) => {
    if (node.type.name !== 'table') return true
    const featured = new Set<number>()
    // Pass 1: glyph cells + detect featured columns from the header row.
    node.forEach((row, rowOffset, rowIndex) => {
      if (row.type.name !== 'table_row') return
      const rowPos = pos + 1 + rowOffset
      let col = 0
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
        // Featured column = a fully-`==highlighted==` cell in the header (the
        // first row). Keyed off row index, not a node-type name (Milkdown's GFM
        // header cells aren't reliably named `table_header`).
        if (rowIndex === 0 && cellFullyHighlighted(cell)) {
          featured.add(col)
        }
        col++
      })
    })
    // Pass 2: tag every cell in a featured column.
    if (featured.size) {
      node.forEach((row, rowOffset) => {
        if (row.type.name !== 'table_row') return
        const rowPos = pos + 1 + rowOffset
        let col = 0
        row.forEach((cell, cellOffset) => {
          const cellPos = rowPos + 1 + cellOffset
          if (featured.has(col)) {
            decos.push(
              Decoration.node(cellPos, cellPos + cell.nodeSize, {
                class: 'tela-cell-featured',
              }),
            )
          }
          col++
        })
      })
    }
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

  // Filter box only earns its place on tables with enough rows to scan.
  if (tbody.rows.length >= 5) {
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
        // Only GFM content tables (Milkdown wraps those in `.tableWrapper`) —
        // NOT block-internal tables like the calendar month grid, which is a
        // <table> too but must never get sort/filter chrome.
        editorView.dom
          .querySelectorAll('.tableWrapper table')
          .forEach((t) => enhanceReadonlyTable(t as HTMLTableElement))
      }
      run()
      return { update: run }
    },
  })
})
