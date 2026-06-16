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
// Spreadsheet exception: Excel / Numbers / LibreOffice Calc put a *rendered
// image* of the copied range on the clipboard alongside the real table (as
// text/html and tab-separated text/plain). Left alone, the image-upload branch
// wins and pastes a picture of the table. So when a paste/drop carries an image
// file AND extractable table data, we build a proper GFM table instead. A single
// `view.dispatch` does this — collab's ySync observes the transaction, so the
// same path works in single and collaborative mode with no second implementation.
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
function tableFromHtml(html: string): string[][] | null {
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
  return rows.length ? rows : null
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

function extractTable(dt: DataTransfer | null): string[][] | null {
  if (!dt) return null
  const html = dt.getData('text/html')
  const fromHtml = html ? tableFromHtml(html) : null
  const rows = fromHtml ?? tableFromTsv(dt.getData('text/plain'))
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
        const files = filesFrom(dt)
        // Spreadsheet paste: prefer the table over its rendered image.
        if (files.some(isImage)) {
          const rows = extractTable(dt)
          if (rows) {
            event.preventDefault()
            insertTable(view, rows)
            return true
          }
        }
        if (files.length === 0) return false
        event.preventDefault()
        void uploadAndInsert(view, pageId, files, view.state.selection.from)
        return true
      },
      handleDOMEvents: {
        drop: (view, event) => {
          const dt = event.dataTransfer
          const files = filesFrom(dt)
          const dropPos = () => {
            const d = view.posAtCoords({ left: event.clientX, top: event.clientY })
            return d?.pos ?? view.state.selection.from
          }
          if (files.some(isImage)) {
            const rows = extractTable(dt)
            if (rows) {
              event.preventDefault()
              insertTable(view, rows, dropPos())
              return true
            }
          }
          if (files.length === 0) return false
          event.preventDefault()
          void uploadAndInsert(view, pageId, files, dropPos())
          return true
        },
      },
    },
  })
}
