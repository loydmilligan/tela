import { $nodeSchema, $prose } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'

// Tabs: a `:::tabs` container directive whose `### Label` sections become tab
// panels. tela renders a tab strip + active panel; in plain markdown it
// degrades to sequential headed sections (all readable, in order). Round-trips
// via mdast-util-directive — the canonical form IS the directive + headings,
// so nothing proprietary is stored.
//
// Schema: `tabs` (content `tab+`) > `tab` (content `block+`, attr `label`).
// The grouping (headings → tabs) happens in the parse runner; the inverse
// (tabs → directive + headings) in the serialize runner. The nodeView adds the
// strip + panel switching (UI-only; not in the doc).

interface MdastNode {
  type: string
  depth?: number
  name?: string
  value?: string
  children?: MdastNode[]
}

function headingText(node: MdastNode): string {
  const parts: string[] = []
  const walk = (n: MdastNode) => {
    if (typeof n.value === 'string') parts.push(n.value)
    n.children?.forEach(walk)
  }
  node.children?.forEach(walk)
  return parts.join('').trim()
}

export const tabSchema = $nodeSchema('tab', () => ({
  group: 'tab',
  content: 'block+',
  defining: true,
  attrs: { label: { default: 'Tab' } },
  parseDOM: [
    {
      tag: 'div[data-tab]',
      getAttrs: (dom) => ({
        label: dom instanceof HTMLElement ? (dom.dataset.label ?? 'Tab') : 'Tab',
      }),
    },
  ],
  toDOM: (node) => [
    'div',
    { 'data-tab': '', 'data-label': node.attrs.label, class: 'tela-tab' },
    0,
  ],
  // Produced/consumed by the parent `tabs` runner — no standalone markdown.
  parseMarkdown: { match: () => false, runner: () => {} },
  toMarkdown: {
    match: (node) => node.type.name === 'tab',
    runner: (state, node) => {
      state.next(node.content)
    },
  },
}))

export const tabsSchema = $nodeSchema('tabs', () => ({
  group: 'block',
  content: 'tab+',
  defining: true,
  parseDOM: [{ tag: 'div[data-tabs]' }],
  toDOM: () => ['div', { 'data-tabs': '', class: 'tela-tabs' }, 0],
  parseMarkdown: {
    match: (node) =>
      node.type === 'containerDirective' && (node as MdastNode).name === 'tabs',
    runner: (state, node, type) => {
      const tabType = type.schema.nodes.tab
      const paraType = type.schema.nodes.paragraph
      const children = (node as MdastNode).children ?? []
      state.openNode(type)
      let tabOpen = false
      let tabHasBlock = false
      const fillEmptyTab = () => {
        if (tabOpen && !tabHasBlock && paraType) {
          state.openNode(paraType)
          state.closeNode()
        }
      }
      for (const child of children) {
        if (child.type === 'heading' && child.depth === 3) {
          if (tabOpen) {
            fillEmptyTab()
            state.closeNode()
          }
          state.openNode(tabType, { label: headingText(child) || 'Tab' })
          tabOpen = true
          tabHasBlock = false
        } else {
          if (!tabOpen) {
            state.openNode(tabType, { label: 'Tab' })
            tabOpen = true
            tabHasBlock = false
          }
          state.next(child as never)
          tabHasBlock = true
        }
      }
      if (tabOpen) {
        fillEmptyTab()
        state.closeNode()
      } else if (paraType) {
        // Empty directive → one empty tab so `tab+`/`block+` are satisfied.
        state.openNode(tabType, { label: 'Tab' })
        state.openNode(paraType)
        state.closeNode()
        state.closeNode()
      }
      state.closeNode()
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'tabs',
    runner: (state, node) => {
      state.openNode('containerDirective', undefined, { name: 'tabs' })
      node.forEach((tab) => {
        state.openNode('heading', undefined, { depth: 3 })
        state.addNode('text', undefined, (tab.attrs.label as string) || 'Tab')
        state.closeNode()
        state.next(tab.content)
      })
      state.closeNode()
    },
  },
}))

// NodeView: a tab strip (built from the child labels) over the panels. Active
// state is UI-only. Mutations on the strip + our display toggles are ignored
// so PM doesn't try to reconcile them; content edits inside panels flow through.
export const tabsNodeView = $prose(() => {
  return new Plugin({
    props: {
      nodeViews: {
        tabs: (node) => {
          const dom = document.createElement('div')
          dom.className = 'tela-tabs'
          const strip = document.createElement('div')
          strip.className = 'tela-tabs-strip'
          strip.setAttribute('contenteditable', 'false')
          const panels = document.createElement('div')
          panels.className = 'tela-tabs-panels'
          dom.appendChild(strip)
          dom.appendChild(panels)
          let active = 0
          let current = node

          const apply = () => {
            Array.from(strip.children).forEach((b, i) => {
              ;(b as HTMLElement).dataset.active = i === active ? 'true' : 'false'
            })
            Array.from(panels.children).forEach((p, i) => {
              ;(p as HTMLElement).style.display = i === active ? '' : 'none'
            })
          }
          const rebuildStrip = () => {
            strip.innerHTML = ''
            current.forEach((tab, _offset, index) => {
              const btn = document.createElement('button')
              btn.type = 'button'
              btn.className = 'tela-tabs-tab'
              btn.textContent = (tab.attrs.label as string) || 'Tab'
              btn.addEventListener('mousedown', (e) => {
                e.preventDefault()
                active = index
                apply()
              })
              strip.appendChild(btn)
            })
          }

          rebuildStrip()
          requestAnimationFrame(apply)

          return {
            dom,
            contentDOM: panels,
            update: (updated) => {
              if (updated.type !== current.type) return false
              current = updated
              if (active >= updated.childCount) active = 0
              rebuildStrip()
              requestAnimationFrame(apply)
              return true
            },
            ignoreMutation: (m) => {
              if (strip.contains(m.target as Node)) return true
              // Our panel show/hide toggles are attribute mutations — ignore so
              // PM doesn't reconcile them; let content edits (childList/text) pass.
              return m.type === 'attributes'
            },
          }
        },
      },
    },
  })
})

// Slash inserter: a two-tab scaffold.
export function insertTabs(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const { schema } = view.state
  const tabsType = schema.nodes.tabs
  const tabType = schema.nodes.tab
  const paraType = schema.nodes.paragraph
  if (!tabsType || !tabType || !paraType) return
  const mkTab = (label: string) =>
    tabType.create({ label }, paraType.create())
  const node = tabsType.create(null, [mkTab('Tab 1'), mkTab('Tab 2')])
  view.dispatch(view.state.tr.replaceSelectionWith(node).scrollIntoView())
  view.focus()
}
