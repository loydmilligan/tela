import { $nodeSchema } from '@milkdown/kit/utils'
import type { EditorView } from '@milkdown/kit/prose/view'
import { Selection } from '@milkdown/kit/prose/state'

// File attachment cards: a `:::file` container directive whose body is the
// file's serve URL, with `name` + `size` carried as directive attributes. Rendered
// as a compact download card (extension badge + name + size). Non-image
// attachments dropped/pasted into the editor become these; images stay native
// `![](url)` nodes.
//
// Round-trips through mdast-util-directive: the canonical markdown is
// `:::file{name="report.pdf" size="248000"}\n<url>\n:::`, so a plain-markdown
// reader still sees a usable URL, and the URL (which embeds the content hash)
// keeps the backend's `embedded` detection working.

interface MdastNode {
  type: string
  name?: string
  value?: string
  attributes?: Record<string, string | null | undefined>
  children?: MdastNode[]
}

interface FileAttrs {
  attrs: { url: string; name: string; size: number }
}

// urlFromDirective pulls the first text node (the file URL) out of a directive body.
function urlFromDirective(node: MdastNode): string {
  let found = ''
  const walk = (n: MdastNode) => {
    if (found) return
    if (n.type === 'text' && n.value) {
      found = n.value.trim()
      return
    }
    n.children?.forEach(walk)
  }
  node.children?.forEach(walk)
  return found
}

function extLabel(name: string): string {
  const i = name.lastIndexOf('.')
  const ext = i >= 0 ? name.slice(i + 1) : ''
  return (ext || 'file').toUpperCase().slice(0, 4)
}

function prettySize(bytes: number): string {
  if (!bytes) return ''
  if (bytes < 1024) return `${bytes} B`
  const kb = bytes / 1024
  if (kb < 1024) return `${kb < 10 ? kb.toFixed(1) : Math.round(kb)} KB`
  const mb = kb / 1024
  return `${mb < 10 ? mb.toFixed(1) : Math.round(mb)} MB`
}

export const fileSchema = $nodeSchema('file', () => ({
  group: 'block',
  atom: true,
  selectable: true,
  attrs: {
    url: { default: '', validate: 'string' },
    name: { default: '', validate: 'string' },
    size: { default: 0 },
  },
  parseDOM: [
    {
      tag: 'a.tela-file',
      getAttrs: (dom) => ({
        url: dom instanceof HTMLElement ? (dom.dataset.url ?? '') : '',
        name: dom instanceof HTMLElement ? (dom.dataset.name ?? '') : '',
        size: dom instanceof HTMLElement ? Number(dom.dataset.size ?? '0') : 0,
      }),
    },
  ],
  toDOM: (node) => {
    const { url, name, size } = (node as unknown as FileAttrs).attrs
    const label = name || url || 'file'
    const children: unknown[] = [
      ['span', { class: 'tela-file-ext' }, extLabel(name || url)],
      ['span', { class: 'tela-file-name' }, label],
    ]
    if (size) children.push(['span', { class: 'tela-file-size' }, prettySize(size)])
    return [
      'a',
      {
        class: 'tela-file',
        href: url || '#',
        download: name || '',
        target: '_blank',
        rel: 'noopener noreferrer',
        contenteditable: 'false',
        'data-url': url,
        'data-name': name,
        'data-size': String(size),
      },
      ...children,
    ] as unknown as readonly [string, ...unknown[]]
  },
  parseMarkdown: {
    match: (node) =>
      node.type === 'containerDirective' && (node as MdastNode).name === 'file',
    runner: (state, node, type) => {
      const attrs = (node as MdastNode).attributes ?? {}
      state.addNode(type, {
        url: urlFromDirective(node as MdastNode),
        name: attrs.name ?? '',
        size: Number(attrs.size ?? '0') || 0,
      })
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'file',
    runner: (state, node) => {
      const url = (node.attrs.url as string) || ''
      state.openNode('containerDirective', undefined, {
        name: 'file',
        attributes: {
          name: (node.attrs.name as string) || '',
          size: String((node.attrs.size as number) || 0),
        },
      })
      state.openNode('paragraph')
      if (url) state.addNode('text', undefined, url)
      state.closeNode()
      state.closeNode()
    },
  },
}))

// insertFileNode places a file card at `pos` (snapped to a valid selection), the
// non-image counterpart to the image insert in the upload plugin.
export function insertFileNode(
  view: EditorView,
  attrs: { url: string; name: string; size: number },
  pos: number,
) {
  const fileType = view.state.schema.nodes.file
  if (!fileType) return
  const node = fileType.create(attrs)
  const at = Math.min(pos, view.state.doc.content.size)
  const sel = Selection.near(view.state.doc.resolve(at))
  view.dispatch(
    view.state.tr.setSelection(sel).replaceSelectionWith(node, false).scrollIntoView(),
  )
}
