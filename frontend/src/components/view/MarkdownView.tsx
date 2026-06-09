import {
  createContext,
  createElement,
  useContext,
  useEffect,
  useMemo,
  useRef,
  type ReactNode,
} from 'react'
import katex from 'katex'
import { refractor } from 'refractor/core'
import { parsePageMarkdown } from '../../lib/markdown/remark-stack'
import { configureRefractor } from '../../lib/milkdown/refractor-config'
import {
  CALLOUT_LABELS,
  type CalloutType,
} from '../../lib/markdown/transforms/callouts'
import { buildMermaidElement } from '../../lib/diagrams/mermaid'
import { buildChartWidget } from '../../lib/diagrams/chart'
import { cn } from '../../lib/utils'

// Context for renderers that need page-scoped data (excalidraw PNG URL today;
// wikilinks/comments later). Provided by MarkdownView.
interface ViewContextValue {
  pageId?: number
}
const ViewContext = createContext<ViewContextValue>({})

// Read-only VIEW renderer (docs/view-edit-split.md): markdown → mdast (shared
// parse stack) → React. No ProseMirror, no Yjs, no editor chunk. It reuses the
// editor's exact parse transforms, refractor grammar set, KaTeX, and the same
// `tela-*` DOM classes, so the output matches the editor by construction.
//
// CSS HOOK (temporary): the content is wrapped in `.tela-milkdown .ProseMirror`
// so the existing reader/editor stylesheets apply verbatim with zero new CSS.
// A later phase extracts a shared `.tela-prose` class and drops the fake
// `.ProseMirror` hook. Until then, render this inside a `.tela-reader` scope to
// get the reading typography (see the Storybook story).
//
// This slice covers the core + callout/highlight/math/code blocks. Directive
// blocks (pull-quote, embed, tabs, kanban, …), wikilinks, collapsibles and
// comments land in subsequent phases; unknown nodes degrade gracefully by
// rendering their children so content is never dropped.

interface MdNode {
  type: string
  children?: MdNode[]
  value?: string
  [k: string]: unknown
}

let refractorReady = false
function ensureRefractor() {
  if (!refractorReady) {
    configureRefractor(refractor)
    refractorReady = true
  }
}

interface HastNode {
  type: string
  tagName?: string
  value?: string
  properties?: Record<string, unknown>
  children?: HastNode[]
}

function renderHast(node: HastNode, key: number): ReactNode {
  if (node.type === 'text') return node.value
  if (node.type === 'element' && node.tagName) {
    const cls = node.properties?.className
    const className = Array.isArray(cls)
      ? cls.join(' ')
      : typeof cls === 'string'
        ? cls
        : undefined
    return createElement(
      node.tagName,
      { key, className },
      node.children?.map((c, i) => renderHast(c, i)),
    )
  }
  return null
}

function CodeBlock({ lang, value }: { lang: string | null; value: string }) {
  ensureRefractor()
  let tree: HastNode | null = null
  if (lang && refractor.registered(lang)) {
    try {
      tree = refractor.highlight(value, lang) as unknown as HastNode
    } catch {
      tree = null
    }
  }
  const langClass = lang ? `language-${lang}` : undefined
  return (
    <pre className={langClass}>
      <code className={langClass}>
        {tree ? tree.children?.map((c, i) => renderHast(c, i)) : value}
      </code>
    </pre>
  )
}

function TexMath({ value, display }: { value: string; display: boolean }) {
  const html = katex.renderToString(value || '', {
    displayMode: display,
    throwOnError: false,
  })
  return display ? (
    <div className="tela-math-block" dangerouslySetInnerHTML={{ __html: html }} />
  ) : (
    <span className="tela-math-inline" dangerouslySetInnerHTML={{ __html: html }} />
  )
}

// Mounts an editor render-core element (mermaid/chart) into the React tree.
// Reuses the exact same builder the editor uses (lib/diagrams/*) — zero drift,
// and the heavy lib (mermaid/echarts) stays lazy.
function DiagramWidget({ kind, code }: { kind: 'mermaid' | 'chart'; code: string }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const host = ref.current
    if (!host) return
    const el = kind === 'mermaid' ? buildMermaidElement(code) : buildChartWidget(code)
    host.appendChild(el)
    return () => {
      try {
        host.removeChild(el)
      } catch {
        /* already detached */
      }
    }
  }, [kind, code])
  return <div ref={ref} />
}

function ExcalidrawView({
  sceneHash,
  altText,
}: {
  sceneHash: string
  altText: string
}) {
  const { pageId } = useContext(ViewContext)
  if (!sceneHash || !pageId) return null
  return (
    <div className="tela-excalidraw" data-scene-hash={sceneHash}>
      <img
        src={`/api/diagrams/${pageId}/${sceneHash}.png`}
        alt={altText || 'Excalidraw diagram'}
        loading="lazy"
      />
    </div>
  )
}

function renderChildren(node: MdNode): ReactNode[] {
  return (node.children ?? []).map((child, i) => renderNode(child, i))
}

function renderNode(node: MdNode, key: number | string): ReactNode {
  switch (node.type) {
    case 'text':
      return node.value
    case 'paragraph':
      return <p key={key}>{renderChildren(node)}</p>
    case 'heading': {
      const depth = Math.min(Math.max(Number(node.depth) || 1, 1), 6)
      return createElement(`h${depth}`, { key }, renderChildren(node))
    }
    case 'strong':
      return <strong key={key}>{renderChildren(node)}</strong>
    case 'emphasis':
      return <em key={key}>{renderChildren(node)}</em>
    case 'delete':
      return <del key={key}>{renderChildren(node)}</del>
    case 'inlineCode':
      return <code key={key}>{node.value}</code>
    case 'break':
      return <br key={key} />
    case 'thematicBreak':
      return <hr key={key} />
    case 'blockquote':
      return <blockquote key={key}>{renderChildren(node)}</blockquote>
    case 'link':
      return (
        <a
          key={key}
          href={String(node.url ?? '')}
          title={node.title ? String(node.title) : undefined}
        >
          {renderChildren(node)}
        </a>
      )
    case 'image':
      return (
        <img
          key={key}
          src={String(node.url ?? '')}
          alt={String(node.alt ?? '')}
          title={node.title ? String(node.title) : undefined}
          loading="lazy"
        />
      )
    case 'list': {
      const ordered = Boolean(node.ordered)
      const start = node.start == null ? undefined : Number(node.start)
      return ordered ? (
        <ol key={key} start={start}>
          {renderChildren(node)}
        </ol>
      ) : (
        <ul key={key}>{renderChildren(node)}</ul>
      )
    }
    case 'listItem': {
      const checked = node.checked
      // Match the editor's task-item DOM: `li[data-item-type=task][data-checked]`
      // — the checkbox is a CSS ::before in the gutter (editor.css), so we reuse
      // it verbatim and the text flows inline.
      if (checked === true || checked === false) {
        return (
          <li key={key} data-item-type="task" data-checked={String(checked)}>
            {renderChildren(node)}
          </li>
        )
      }
      return <li key={key}>{renderChildren(node)}</li>
    }
    case 'code': {
      const lang = node.lang == null ? null : String(node.lang)
      const value = String(node.value ?? '')
      // mermaid / chart render as diagrams (view shows only the result, not the
      // source) via the shared editor render cores.
      if (lang === 'mermaid') return <DiagramWidget key={key} kind="mermaid" code={value} />
      if (lang === 'chart') return <DiagramWidget key={key} kind="chart" code={value} />
      return <CodeBlock key={key} lang={lang} value={value} />
    }
    case 'excalidraw':
      return (
        <ExcalidrawView
          key={key}
          sceneHash={String(node.sceneHash ?? '')}
          altText={String(node.altText ?? '')}
        />
      )
    case 'table': {
      const align = (node.align as (string | null)[] | undefined) ?? []
      const rows = node.children ?? []
      const [head, ...bodyRows] = rows
      return (
        <table key={key}>
          {head ? (
            <thead>
              <tr>
                {(head.children ?? []).map((cell, i) => (
                  <th key={i} style={alignStyle(align[i])}>
                    {renderChildren(cell)}
                  </th>
                ))}
              </tr>
            </thead>
          ) : null}
          <tbody>
            {bodyRows.map((row, r) => (
              <tr key={r}>
                {(row.children ?? []).map((cell, i) => (
                  <td key={i} style={alignStyle(align[i])}>
                    {renderChildren(cell)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      )
    }
    case 'callout': {
      const type = calloutType(node.calloutType)
      return (
        <div
          key={key}
          className={`tela-callout tela-callout-${type}`}
          data-callout-type={type}
        >
          <div className="tela-callout-header">
            <span
              className="tela-callout-icon"
              data-callout-icon={type}
              aria-hidden="true"
            />
            <span className="tela-callout-label">{CALLOUT_LABELS[type]}</span>
          </div>
          <div className="tela-callout-body">{renderChildren(node)}</div>
        </div>
      )
    }
    case 'highlight':
      return (
        <mark key={key} className="tela-highlight">
          {renderChildren(node)}
        </mark>
      )
    case 'math':
      return <TexMath key={key} value={String(node.value ?? '')} display />
    case 'inlineMath':
      return <TexMath key={key} value={String(node.value ?? '')} display={false} />
    case 'html':
      // Raw-HTML / collapsibles handling lands in a later phase. Drop for now
      // rather than dangerously injecting arbitrary markup.
      return null
    default:
      // Unknown (e.g. directive blocks, footnotes, wikilinks) — degrade
      // gracefully by rendering children so no content is lost. Each gets a
      // real renderer in a later phase, gated by the manifest view-renderer
      // requirement (docs/view-edit-split.md).
      return node.children ? <span key={key}>{renderChildren(node)}</span> : null
  }
}

function alignStyle(a: string | null | undefined) {
  return a ? { textAlign: a as 'left' | 'right' | 'center' } : undefined
}

function calloutType(raw: unknown): CalloutType {
  const t = typeof raw === 'string' ? raw : 'note'
  return (CALLOUT_LABELS as Record<string, string>)[t] ? (t as CalloutType) : 'note'
}

export function MarkdownView({
  body,
  pageId,
  className,
}: {
  body: string
  /** Page id — needed to build the excalidraw PNG URL (and later wikilinks). */
  pageId?: number
  className?: string
}) {
  const tree = useMemo(() => parsePageMarkdown(body), [body])
  const ctx = useMemo<ViewContextValue>(() => ({ pageId }), [pageId])
  return (
    <ViewContext.Provider value={ctx}>
      <div className={cn('tela-milkdown', className)}>
        {/* Temporary `.ProseMirror` CSS hook — see file header. `whiteSpace:
            normal` overrides the editor's `pre-wrap` so markdown soft-wraps
            collapse to spaces (correct for a static HTML view) instead of
            rendering as hard line breaks. Drops out with the `.tela-prose`
            extraction. */}
        <div className="ProseMirror" data-tela-view="" style={{ whiteSpace: 'normal' }}>
          {renderChildren(tree as unknown as MdNode)}
        </div>
      </div>
    </ViewContext.Provider>
  )
}
