import { $nodeSchema, $prose } from '@milkdown/kit/utils'
import { editorViewCtx } from '@milkdown/kit/core'
import { Plugin } from '@milkdown/kit/prose/state'
import type { EditorView } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'
import type { Ctx } from '@milkdown/ctx'
import {
  buildBookmarkDOM,
  bookshelfUrlsFromDirective,
  type BookshelfDirectiveNode,
} from '../../lib/blocks/bookmark'
import { insertBlock } from '../../lib/milkdown/insert-block'

// Bookshelf: a container of bookmark cards for a group of URLs.
//
// Canonical markdown is a `:::bookshelf` directive whose body is a plain
// bullet list of links — a markdown reader without tela just sees a link
// list, and an agent writes one without learning a new grammar:
//
//   :::bookshelf{style="expanded"}
//   * <https://example.com/a>
//   * [optional title](https://example.com/b)
//   :::
//
// `style`: `expanded` (full-detail cards in a responsive grid; narrow
// screens fall back to a scroll-snap carousel — pure CSS) | `compact`
// (dense rows). The card look + unfurl fetch live in lib/blocks/bookmark.ts,
// shared with the read-only view renderer.
//
// Schema: atom block with attrs {style, urls}. The editor nodeView renders
// the cards and owns light editing (add link, remove card, toggle style) via
// setNodeMarkup — no nested-editable region, so collab/y-prosemirror sees a
// single attr change per edit.

export type BookshelfStyle = 'expanded' | 'compact'

function normStyle(raw: unknown): BookshelfStyle {
  return raw === 'compact' ? 'compact' : 'expanded'
}

export const bookshelfSchema = $nodeSchema('bookshelf', () => ({
  group: 'block',
  atom: true,
  selectable: true,
  attrs: {
    style: { default: 'expanded', validate: 'string' },
    urls: { default: [] as string[] },
  },
  parseDOM: [
    {
      tag: 'div.tela-bookshelf',
      getAttrs: (dom) => {
        const el = dom as HTMLElement
        let urls: string[] = []
        try {
          const parsed: unknown = JSON.parse(el.dataset.urls ?? '[]')
          if (Array.isArray(parsed)) urls = parsed.filter((u): u is string => typeof u === 'string')
        } catch {
          // malformed data attr → empty shelf
        }
        return { style: normStyle(el.dataset.style), urls }
      },
    },
  ],
  toDOM: (node) => {
    // Static fallback (clipboard serialization etc.) — the nodeView below is
    // what actually renders in the editor.
    const { style, urls } = node.attrs as { style: BookshelfStyle; urls: string[] }
    return [
      'div',
      {
        class: 'tela-bookshelf',
        'data-style': style,
        'data-urls': JSON.stringify(urls),
        contenteditable: 'false',
      },
    ]
  },
  parseMarkdown: {
    match: (node) =>
      node.type === 'containerDirective' && (node as BookshelfDirectiveNode).name === 'bookshelf',
    runner: (state, node, type) => {
      const dir = node as BookshelfDirectiveNode
      state.addNode(type, {
        style: normStyle(dir.attributes?.style),
        urls: bookshelfUrlsFromDirective(dir),
      })
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'bookshelf',
    runner: (state, node) => {
      const { style, urls } = node.attrs as { style: BookshelfStyle; urls: string[] }
      state.openNode('containerDirective', undefined, {
        name: 'bookshelf',
        attributes: { style },
      })
      state.openNode('list', undefined, { ordered: false })
      for (const url of urls) {
        state.openNode('listItem')
        state.openNode('paragraph')
        // text === url → mdast-util-to-markdown emits a clean `<url>` autolink.
        state.openNode('link', undefined, { url })
        state.addNode('text', undefined, url)
        state.closeNode()
        state.closeNode()
        state.closeNode()
      }
      state.closeNode()
      state.closeNode()
    },
  },
}))

// ── editor nodeView: cards + light editing ──────────────────────────────────

function setShelfAttrs(
  view: EditorView,
  getPos: () => number | undefined,
  attrs: { style: BookshelfStyle; urls: string[] },
) {
  const pos = getPos()
  if (pos == null) return
  view.dispatch(view.state.tr.setNodeMarkup(pos, undefined, attrs))
}

function renderShelf(
  dom: HTMLElement,
  node: ProseNode,
  view: EditorView,
  getPos: () => number | undefined,
) {
  const { style, urls } = node.attrs as { style: BookshelfStyle; urls: string[] }
  dom.className = 'tela-bookshelf'
  dom.dataset.style = style
  dom.dataset.urls = JSON.stringify(urls)
  dom.replaceChildren()

  const grid = document.createElement('div')
  grid.className = 'tela-bookshelf-grid'
  dom.appendChild(grid)
  for (const url of urls) {
    const item = document.createElement('div')
    item.className = 'tela-bookshelf-item'
    item.appendChild(buildBookmarkDOM(url, style === 'compact' ? 'row' : 'card'))
    if (view.editable) {
      const remove = document.createElement('button')
      remove.type = 'button'
      remove.className = 'tela-bookshelf-remove'
      remove.title = 'Remove bookmark'
      remove.textContent = '×'
      remove.onclick = (e) => {
        e.preventDefault()
        setShelfAttrs(view, getPos, { style, urls: urls.filter((u) => u !== url) })
      }
      item.appendChild(remove)
    }
    grid.appendChild(item)
  }
  if (urls.length === 0) {
    const empty = document.createElement('span')
    empty.className = 'tela-bookshelf-empty'
    empty.textContent = 'Empty bookshelf — add a link'
    grid.appendChild(empty)
  }

  if (view.editable) {
    const bar = document.createElement('div')
    bar.className = 'tela-bookshelf-bar'
    const add = document.createElement('button')
    add.type = 'button'
    add.className = 'tela-bookshelf-btn'
    add.textContent = '+ Add link'
    add.onclick = (e) => {
      e.preventDefault()
      const raw = window.prompt('Bookmark URL(s) — separate several with spaces:')
      if (!raw) return
      const next = raw.split(/[\s,]+/).filter((u) => /^https?:\/\//.test(u))
      if (!next.length) return
      setShelfAttrs(view, getPos, { style, urls: [...new Set([...urls, ...next])] })
    }
    const toggle = document.createElement('button')
    toggle.type = 'button'
    toggle.className = 'tela-bookshelf-btn'
    toggle.textContent = style === 'compact' ? 'Expanded view' : 'Compact view'
    toggle.onclick = (e) => {
      e.preventDefault()
      setShelfAttrs(view, getPos, {
        style: style === 'compact' ? 'expanded' : 'compact',
        urls,
      })
    }
    bar.append(add, toggle)
    dom.appendChild(bar)
  }
}

function createBookshelfNodeViewPlugin(): Plugin {
  return new Plugin({
    props: {
      nodeViews: {
        bookshelf: (node, view, getPos) => {
          const dom = document.createElement('div')
          dom.contentEditable = 'false'
          renderShelf(dom, node, view, getPos)
          return {
            dom,
            update: (next) => {
              if (next.type !== node.type) return false
              renderShelf(dom, next, view, getPos)
              return true
            },
          }
        },
      },
    },
  })
}

// Slash inserter: prompt for one or more URLs; empty prompt inserts an empty
// shelf the user fills via "+ Add link".
export function insertBookshelf(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const raw = window.prompt('Bookshelf URLs (separate several with spaces):', '')
  if (raw == null) return
  const urls = raw.split(/[\s,]+/).filter((u) => /^https?:\/\//.test(u))
  const type = view.state.schema.nodes.bookshelf
  if (!type) return
  insertBlock(view, type.create({ style: 'expanded', urls }), { caret: 'none' })
}

export const bookshelfNodeView = $prose(createBookshelfNodeViewPlugin)
