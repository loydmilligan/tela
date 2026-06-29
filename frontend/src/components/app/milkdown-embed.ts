import { $nodeSchema } from '@milkdown/kit/utils'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'
import { embedIframeSrc } from '../../lib/markdown/embed'
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
