import { $nodeSchema, $prose } from '@milkdown/kit/utils'
import { editorViewCtx } from '@milkdown/kit/core'
import { Plugin } from '@milkdown/kit/prose/state'
import type { Ctx } from '@milkdown/ctx'
import { embedIframeSrc } from '../../lib/markdown/embed'
import { buildBookmarkDOM } from '../../lib/blocks/bookmark'
import { insertBlock } from '../../lib/milkdown/insert-block'
// Provider resolution lives in lib/markdown/embed.ts (Milkdown-free, shared with
// the view renderer); re-export so existing importers (the story) keep working.
export { embedIframeSrc }

// Web embeds: a `:::embed` container directive whose body is a single URL,
// rendered as a responsive, sandboxed iframe for a tight allowlist of providers
// (YouTube, Vimeo, Loom). Anything else degrades to a plain link card — we never
// iframe an arbitrary origin, and never inject third-party scripts (so tweets /
// gists, which need their platform JS, render as links, not embeds).
//
// Round-trips through mdast-util-directive: the canonical markdown is
// `:::embed\n<url>\n:::`, so plain-markdown readers just see the URL.
//
// Schema: `embed` (group block, atom, attr `url`). toDOM computes the provider
// iframe src; the markdown runners carry the URL as the directive's text body.

interface MdastNode {
  type: string
  name?: string
  value?: string
  children?: MdastNode[]
}

interface EmbedAttrs {
  attrs: { url: string }
}


function urlFromDirective(node: MdastNode): string {
  // The URL is the directive's text body. Walk for the first non-empty text.
  let found = ''
  const walk = (n: MdastNode) => {
    if (found) return
    if (n.type === 'text' && typeof n.value === 'string' && n.value.trim()) {
      found = n.value.trim()
      return
    }
    n.children?.forEach(walk)
  }
  node.children?.forEach(walk)
  return found
}

export const embedSchema = $nodeSchema('embed', () => ({
  group: 'block',
  atom: true,
  selectable: true,
  attrs: {
    url: { default: '', validate: 'string' },
  },
  parseDOM: [
    {
      tag: 'div.tela-embed',
      getAttrs: (dom) => ({
        url: dom instanceof HTMLElement ? (dom.dataset.url ?? '') : '',
      }),
    },
  ],
  toDOM: (node) => {
    const { url } = (node as unknown as EmbedAttrs).attrs
    const src = embedIframeSrc(url)
    if (src) {
      return [
        'div',
        { class: 'tela-embed', 'data-url': url, contenteditable: 'false' },
        [
          'iframe',
          {
            src,
            loading: 'lazy',
            allow:
              'accelerometer; encrypted-media; gyroscope; picture-in-picture; fullscreen',
            allowfullscreen: 'true',
            referrerpolicy: 'strict-origin-when-cross-origin',
            sandbox: 'allow-scripts allow-same-origin allow-popups allow-presentation',
          },
        ],
      ]
    }
    // Unknown provider (or empty) → a safe link card, not an iframe.
    return [
      'div',
      {
        class: 'tela-embed tela-embed-link',
        'data-url': url,
        contenteditable: 'false',
      },
      url
        ? [
            'a',
            { href: url, target: '_blank', rel: 'noopener noreferrer nofollow' },
            url,
          ]
        : ['span', { class: 'tela-embed-empty' }, 'Empty embed'],
    ]
  },
  parseMarkdown: {
    match: (node) =>
      node.type === 'containerDirective' && (node as MdastNode).name === 'embed',
    runner: (state, node, type) => {
      state.addNode(type, { url: urlFromDirective(node as MdastNode) })
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'embed',
    runner: (state, node) => {
      const url = (node.attrs.url as string) || ''
      state.openNode('containerDirective', undefined, { name: 'embed' })
      state.openNode('paragraph')
      if (url) state.addNode('text', undefined, url)
      state.closeNode()
      state.closeNode()
    },
  },
}))

// Editor nodeView: providers keep the schema toDOM iframe; every other URL
// upgrades from the bare link line to a real bookmark card (unfurl title +
// host + favicon + description via lib/blocks/bookmark.ts — shared with the
// view renderer). A nodeView (not toDOM) because the card fills in async;
// PM re-runs toDOM on every redraw, which would refire the skeleton.
// The outer div keeps `.tela-embed` + data-url so parseDOM, the selection
// ring, and copy/paste round-trip are unchanged.
function createEmbedNodeViewPlugin(): Plugin {
  return new Plugin({
    props: {
      nodeViews: {
        embed: (node) => {
          const url = (node.attrs.url as string) || ''
          const dom = document.createElement('div')
          dom.dataset.url = url
          dom.contentEditable = 'false'
          if (embedIframeSrc(url)) {
            // Provider path: defer to the schema toDOM structure by building
            // the same iframe here (a nodeView fully replaces toDOM).
            dom.className = 'tela-embed'
            const iframe = document.createElement('iframe')
            iframe.src = embedIframeSrc(url)!
            iframe.loading = 'lazy'
            iframe.allow =
              'accelerometer; encrypted-media; gyroscope; picture-in-picture; fullscreen'
            iframe.allowFullscreen = true
            iframe.referrerPolicy = 'strict-origin-when-cross-origin'
            iframe.setAttribute(
              'sandbox',
              'allow-scripts allow-same-origin allow-popups allow-presentation',
            )
            dom.appendChild(iframe)
          } else if (url) {
            dom.className = 'tela-embed tela-embed-bookmark'
            dom.appendChild(buildBookmarkDOM(url, 'card'))
          } else {
            dom.className = 'tela-embed tela-embed-link'
            const empty = document.createElement('span')
            empty.className = 'tela-embed-empty'
            empty.textContent = 'Empty embed'
            dom.appendChild(empty)
          }
          return {
            dom,
            // URL changed (e.g. collab peer edit) → rebuild from scratch.
            update: (next) => next.type === node.type && next.attrs.url === url,
          }
        },
      },
    },
  })
}

// Slash inserter: prompt for a URL, insert an embed (link card until it resolves
// to a known provider). A bare prompt keeps it dependency-free; paste-unfurl
// stays the richer path for casual links.
export function insertEmbed(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const url = window.prompt('Embed URL (YouTube, Vimeo, Loom, or any link):')
  if (url == null) return
  const trimmed = url.trim()
  if (!trimmed) return
  const embedType = view.state.schema.nodes.embed
  if (!embedType) return
  const node = embedType.create({ url: trimmed })
  insertBlock(view, node, { caret: 'none' })
}

export const embedNodeView = $prose(createEmbedNodeViewPlugin)
