import { Plugin } from '@milkdown/kit/prose/state'
import type { EditorView } from '@milkdown/kit/prose/view'

// Paste-a-bare-URL → `[title](url)`. When the clipboard holds nothing but a
// single http(s) URL and the selection is empty, fetch the page title via the
// SSRF-guarded /api/unfurl endpoint and insert a titled link instead of a raw
// URL. Falls back to the URL as its own text if the title can't be fetched.
//
// Markdown-canonical: this is a stock `[text](href)` link — no proprietary
// embed/bookmark card (those don't round-trip). Wired only in editable,
// non-share mode, and AFTER the mira paste-hook so mira URLs still import.

const URL_RE = /^https?:\/\/[^\s]+$/

function insertTitledLink(view: EditorView, from: number, url: string, title: string) {
  const linkType = view.state.schema.marks.link
  if (!linkType) return
  const at = Math.min(from, view.state.doc.content.size)
  const mark = linkType.create({ href: url, title: null })
  const linkText = view.state.schema.text(title || url, [mark])
  // Trailing unmarked space so typing after the link doesn't inherit the mark.
  const spaceText = view.state.schema.text(' ', [])
  view.dispatch(
    view.state.tr
      .replaceWith(at, at, [linkText, spaceText])
      .setStoredMarks([])
      .scrollIntoView(),
  )
}

async function unfurlAndInsert(view: EditorView, url: string, from: number) {
  let title = ''
  try {
    const res = await fetch(`/api/unfurl?url=${encodeURIComponent(url)}`, {
      credentials: 'include',
    })
    if (res.ok) {
      const data = (await res.json()) as { title?: string }
      title = (data.title ?? '').trim()
    }
  } catch {
    // Network error — fall through to a plain link (title = '').
  }
  insertTitledLink(view, from, url, title)
}

export function createUrlUnfurlPlugin(): Plugin {
  return new Plugin({
    props: {
      handlePaste: (view, event) => {
        if (!view.state.selection.empty) return false
        const text = event.clipboardData?.getData('text/plain')?.trim() ?? ''
        if (!URL_RE.test(text)) return false
        event.preventDefault()
        void unfurlAndInsert(view, text, view.state.selection.from)
        return true
      },
    },
  })
}
