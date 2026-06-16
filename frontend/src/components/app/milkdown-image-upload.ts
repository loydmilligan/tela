import { Plugin } from '@milkdown/kit/prose/state'
import { Selection } from '@milkdown/kit/prose/state'
import { DOMParser } from '@milkdown/kit/prose/model'
import type { EditorView } from '@milkdown/kit/prose/view'
import { uploadAttachment } from '../../lib/queries/attachments'
import { insertFileNode } from './milkdown-file'

// Drop / paste upload UX. Any file dropped or pasted into the editor is uploaded
// to POST /api/pages/{pageId}/attachments (the unified space_files store) and
// inserted inline: images as a standard `![alt](url)` node, everything else as a
// `:::file` card. The body stays canonical markdown. Wired only in editable,
// non-share, page-known mode (see milkdown-editor.tsx). URL pastes / rich HTML
// fall through to the existing handlers (only file payloads are intercepted).
//
// Table exception (see pastedTable): Excel / Numbers / Calc put a *rendered
// image* of the copied range on the clipboard alongside the real table; left
// alone, the image-upload branch wins and pastes a picture. Google Sheets and
// many web tables instead ship an all-<td> HTML table the default GFM parser
// can't header — it builds a broken table. In both cases we build a proper GFM
// table ourselves. A single `view.dispatch` does this — collab's ySync observes
// the transaction, so the same path works in single and collaborative mode with
// no second implementation.
//
// After each upload a `tela:attachments-changed` event fires so the page's
// AttachmentStrip refetches (the new file is now parented to the page).

function filesFrom(dt: DataTransfer | null): File[] {
  if (!dt) return []
  return Array.from(dt.files)
}

function isImage(file: File): boolean {
  return file.type.startsWith('image/')
}

function cellText(el: Element): string {
  return (el.textContent ?? '').replace(/\s+/g, ' ').trim()
}

// Structurally unambiguous: rows are <tr>, cells are <th>/<td>. Scoped queries
// keep a nested table inside a cell from contaminating the outer row/cell list.
// `hasHeader` distinguishes tables the default GFM clipboard parser can handle
// (a real <th> / data-is-header first row) from all-<td> tables (Google Sheets,
// many web tables) that it turns into a broken/headerless mess — the latter we
// rebuild ourselves with an explicit header row.
function htmlTable(html: string): { rows: string[][]; hasHeader: boolean } | null {
  if (!/<table[\s>]/i.test(html)) return null
  const doc = new window.DOMParser().parseFromString(html, 'text/html')
  const table = doc.querySelector('table')
  if (!table) return null
  const trs = table.querySelectorAll(':scope > tr, :scope > thead > tr, :scope > tbody > tr')
  const rows: string[][] = []
  trs.forEach((tr) => {
    const cells = Array.from(tr.querySelectorAll(':scope > th, :scope > td')).map(cellText)
    if (cells.length) rows.push(cells)
  })
  if (!rows.length) return null
  const firstRow = table.querySelector('tr')
  const hasHeader =
    !!firstRow && (firstRow.matches('[data-is-header]') || firstRow.querySelector('th') != null)
  return { rows, hasHeader }
}

// Fallback for sources that ship TSV but no table HTML. Excel separates rows
// with \r; normalise to \n first. Quoted multiline cells aren't handled — the
// HTML path (preferred) covers those.
function tableFromTsv(text: string): string[][] | null {
  if (!text.includes('\t')) return null
  const lines = text
    .replace(/\r\n?/g, '\n')
    .split('\n')
    .filter((l) => l.length > 0)
  if (lines.length === 0) return null
  return lines.map((l) => l.split('\t'))
}

// GFM's table node is `table_header_row table_row+`: it needs a header AND at
// least one body row, and at least 2 columns to read as tabular. Ragged rows
// are padded to the widest. Returns null when the payload isn't table-shaped.
function normalizeTable(rows: string[][]): string[][] | null {
  const width = Math.max(...rows.map((r) => r.length))
  if (width < 2 || rows.length < 2) return null
  return rows.map((r) => {
    const padded = r.slice(0, width)
    while (padded.length < width) padded.push('')
    return padded
  })
}

// Decide whether a paste/drop should become a GFM table, and return its rows.
// We take over when either:
//  (a) an image file rides along — spreadsheets (Excel/Numbers/Calc) put a
//      rendered picture of the range on the clipboard next to the real table; or
//  (b) the clipboard has an HTML <table> with no header row the GFM parser can
//      use (all-<td>, e.g. Google Sheets) — the default would build it broken.
// A proper-header HTML table (real <th>) is left to the default handler, which
// preserves rich inline cell content (links, bold) our text rebuild would flatten.
function pastedTable(dt: DataTransfer | null): string[][] | null {
  if (!dt) return null
  const html = dt.getData('text/html')
  const ht = html ? htmlTable(html) : null
  const takeover = filesFrom(dt).some(isImage) || (ht != null && !ht.hasHeader)
  if (!takeover) return null
  const rows = ht?.rows ?? tableFromTsv(dt.getData('text/plain'))
  return rows ? normalizeTable(rows) : null
}

// Build a clean <table> (first row → <th>, rest → <td>) and parse it through the
// schema's DOMParser. We emit our own <th> header because spreadsheet HTML marks
// the header row only by styling — every cell is a <td> — which GFM's
// header-row parseDOM (it looks for a <th>) would otherwise reject.
function insertTable(view: EditorView, rows: string[][], pos?: number) {
  const schema = view.state.schema
  const table = document.createElement('table')
  const thead = document.createElement('thead')
  const htr = document.createElement('tr')
  for (const c of rows[0]) {
    const th = document.createElement('th')
    th.textContent = c
    htr.appendChild(th)
  }
  thead.appendChild(htr)
  table.appendChild(thead)
  const tbody = document.createElement('tbody')
  for (const row of rows.slice(1)) {
    const tr = document.createElement('tr')
    for (const c of row) {
      const td = document.createElement('td')
      td.textContent = c
      tr.appendChild(td)
    }
    tbody.appendChild(tr)
  }
  table.appendChild(tbody)
  const container = document.createElement('div')
  container.appendChild(table)
  const slice = DOMParser.fromSchema(schema).parseSlice(container)

  let tr = view.state.tr
  if (pos != null) {
    const at = Math.min(pos, view.state.doc.content.size)
    tr = tr.setSelection(Selection.near(view.state.doc.resolve(at)))
  }
  view.dispatch(tr.replaceSelection(slice).scrollIntoView())
}

async function uploadAndInsert(
  view: EditorView,
  pageId: number,
  files: File[],
  pos: number,
) {
  for (const file of files) {
    try {
      const a = await uploadAttachment(pageId, file)
      if (isImage(file)) {
        const imageType = view.state.schema.nodes.image
        if (!imageType) continue
        const alt = file.name.replace(/\.[^.]+$/, '')
        const node = imageType.create({ src: a.url, alt })
        const at = Math.min(pos, view.state.doc.content.size)
        const sel = Selection.near(view.state.doc.resolve(at))
        view.dispatch(
          view.state.tr.setSelection(sel).replaceSelectionWith(node, false).scrollIntoView(),
        )
      } else {
        insertFileNode(view, { url: a.url, name: a.name, size: a.byte_size }, pos)
      }
      window.dispatchEvent(
        new CustomEvent('tela:attachments-changed', { detail: { pageId } }),
      )
    } catch {
      // Best-effort: a failed upload just doesn't insert. The user can retry.
    }
  }
}

export function createAttachmentDropPlugin(pageId: number): Plugin {
  return new Plugin({
    props: {
      handlePaste: (view, event) => {
        const dt = event.clipboardData
        // Spreadsheet / headerless-HTML table → real GFM table.
        const rows = pastedTable(dt)
        if (rows) {
          event.preventDefault()
          insertTable(view, rows)
          return true
        }
        const files = filesFrom(dt)
        if (files.length === 0) return false
        event.preventDefault()
        void uploadAndInsert(view, pageId, files, view.state.selection.from)
        return true
      },
      handleDOMEvents: {
        drop: (view, event) => {
          const dt = event.dataTransfer
          const dropPos = () => {
            const d = view.posAtCoords({ left: event.clientX, top: event.clientY })
            return d?.pos ?? view.state.selection.from
          }
          const rows = pastedTable(dt)
          if (rows) {
            event.preventDefault()
            insertTable(view, rows, dropPos())
            return true
          }
          const files = filesFrom(dt)
          if (files.length === 0) return false
          event.preventDefault()
          void uploadAndInsert(view, pageId, files, dropPos())
          return true
        },
      },
    },
  })
}
