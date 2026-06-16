import { Plugin } from '@milkdown/kit/prose/state'
import { Selection } from '@milkdown/kit/prose/state'
import type { EditorView } from '@milkdown/kit/prose/view'
import { embedIframeSrc } from '../../lib/markdown/embed'

// Paste a bare http(s) URL (and nothing else, with an empty selection) → smart
// insert based on what the URL points at:
//   • a known embed provider (YouTube / Vimeo / Loom) → an `:::embed` player;
//   • a direct image URL (…/foo.png) → an inline `![](url)` image;
//   • anything else → `[title](url)`, the page title fetched via the
//     SSRF-guarded /api/unfurl endpoint (falls back to the URL as its own text).
//
// All three are markdown-canonical (embed round-trips as `:::embed`, the rest are
// stock image/link nodes) — no proprietary bookmark card. Each insert is a normal
// `view.dispatch`, so collab's ySync picks it up; wired editable, non-share only.

const URL_RE = /^https?:\/\/[^\s]+$/
const IMAGE_EXT_RE = /\.(png|jpe?g|gif|webp|svg|avif|bmp|ico)$/i

function isImageUrl(raw: string): boolean {
  try {
    return IMAGE_EXT_RE.test(new URL(raw).pathname)
  } catch {
    return false
  }
}

// Last path segment, extension stripped, percent-decoded → a readable alt.
function altFromUrl(raw: string): string {
  try {
    const seg = new URL(raw).pathname.split('/').filter(Boolean).pop() ?? ''
    return decodeURIComponent(seg).replace(/\.[^.]+$/, '')
  } catch {
    return ''
  }
}

function insertEmbed(view: EditorView, url: string): boolean {
  const embedType = view.state.schema.nodes.embed
  if (!embedType) return false
  view.dispatch(view.state.tr.replaceSelectionWith(embedType.create({ url })).scrollIntoView())
  return true
}

function insertImage(view: EditorView, from: number, url: string): boolean {
  const imageType = view.state.schema.nodes.image
  if (!imageType) return false
  const node = imageType.create({ src: url, alt: altFromUrl(url) })
  const at = Math.min(from, view.state.doc.content.size)
  const sel = Selection.near(view.state.doc.resolve(at))
  view.dispatch(view.state.tr.setSelection(sel).replaceSelectionWith(node, false).scrollIntoView())
  return true
}

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
        const from = view.state.selection.from
        // Known video provider → embed player.
        if (embedIframeSrc(text) && insertEmbed(view, text)) {
          event.preventDefault()
          return true
        }
        // Direct image URL → inline image.
        if (isImageUrl(text) && insertImage(view, from, text)) {
          event.preventDefault()
          return true
        }
        // Everything else → titled link (async title fetch).
        event.preventDefault()
        void unfurlAndInsert(view, text, from)
        return true
      },
    },
  })
}
