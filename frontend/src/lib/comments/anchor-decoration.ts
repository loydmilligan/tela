import { $ctx } from '@milkdown/kit/utils'
import type { Ctx } from '@milkdown/kit/ctx'
import { Plugin, type EditorState } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet, type EditorView } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'
import { resolveAnchor, type CommentAnchor } from './anchor'
import type { CommentThread } from './use-comments'

// In-body anchor decoration for comment threads. Resolves text-fingerprint
// anchors against `view.state.doc.textBetween(0, doc.content.size, '\n')` —
// the same projection lib/comments/anchor.ts captureAnchor uses. Diverging
// here would silently desync tier-1 prefix/suffix matching offsets.
//
// Pure markdown / pure DOM module — never imports Yjs. During live collab the
// PM doc reflects the Y.Doc projection, so resolving against the PM doc is
// the correct hand-off and keeps the comments subsystem off the Yjs API.

// Slice carrying the live thread list (TanStack queryKey
// ['comments','page',pageId]). React side pushes via ctx.set() + a meta-flagged
// no-op tx whenever the data ref changes; the PM plugin reads on rebuild.
// `null` = "no data yet" (loading). Empty array = "no threads on this page".
export const commentThreadsCtx = $ctx<CommentThread[] | null, 'commentThreads'>(
  null,
  'commentThreads',
)

// React-side callback bundle. Two callbacks live together because both flow
// through the same React render cycle (PageView owns commentsOpen + the
// orphan-ids set together). Wrapped in a single slice so updates are atomic.
export interface CommentAnchorCallbacks {
  // Body-click → panel coordination. Fired when the user clicks any
  // .tela-comment-anchor span; React side opens the Sheet (if closed) and
  // scrolls/flashes the matching panel row.
  onAnchorClick: (threadId: number) => void
  // Reports each thread's current resolution against the live PM text so
  // the panel can render orphaned threads with an "Orphaned" tag. Called
  // after every decoration rebuild.
  onResolved: (
    resolutions: Map<number, { from: number; to: number } | null>,
  ) => void
}

export const commentAnchorCallbacksCtx = $ctx<
  CommentAnchorCallbacks | null,
  'commentAnchorCallbacks'
>(null, 'commentAnchorCallbacks')

// Tx meta tag. Set by:
// (a) the plugin's own debounced doc-change scheduler (auto-rebuild on edit), and
// (b) the React effect that pushes a new threads array into the ctx slice
//     (immediate rebuild on thread-list change).
export const COMMENT_ANCHOR_META = 'tela-comment-anchor-rebuild'

interface CommentAnchorPluginState {
  decos: DecorationSet
}

// Debounce typing-driven rebuilds so resolveAnchor doesn't run for every
// thread on every keystroke. 250ms is the brief's recommended default; if
// profiling shows it's still expensive on >50KB bodies, the fallback is to
// rebuild only on blur + on initial load (panel cache still updates live via
// its own TanStack subscription).
const REBUILD_DEBOUNCE_MS = 250

// Avoid running during Milkdown's first ~200ms of mount churn (parser
// warm-up + ySync plugin's initial fragment pull in collab mode). Without
// this delay the first paint can land before the doc is in PM state, causing
// a one-tick orphan-everything flash.
const INITIAL_MOUNT_DELAY_MS = 200

export function createCommentAnchorPlugin(
  ctx: Ctx,
): Plugin<CommentAnchorPluginState> {
  return new Plugin<CommentAnchorPluginState>({
    state: {
      init: () => ({ decos: DecorationSet.empty }),
      apply: (tr, old) => {
        const rebuild = tr.getMeta(COMMENT_ANCHOR_META) === true
        if (!rebuild) {
          // Map existing decorations through the tx so positions stay sane
          // between rebuilds. Cheap no-op if !tr.docChanged.
          return { decos: old.decos.map(tr.mapping, tr.doc) }
        }
        const threads = ctx.get(commentThreadsCtx.key)
        const callbacks = ctx.get(commentAnchorCallbacksCtx.key)
        return { decos: buildDecorations(tr.doc, threads, callbacks) }
      },
    },
    view: (view) => {
      let debounceHandle: number | null = null
      let mountDelayHandle: number | null = null
      let ready = false

      mountDelayHandle = window.setTimeout(() => {
        mountDelayHandle = null
        ready = true
        // First paint — pick up whatever threads landed during the delay.
        dispatchRebuild(view)
      }, INITIAL_MOUNT_DELAY_MS)

      return {
        update: (view, prevState) => {
          if (!ready) return
          if (view.state.doc.eq(prevState.doc)) return
          if (debounceHandle != null) window.clearTimeout(debounceHandle)
          debounceHandle = window.setTimeout(() => {
            debounceHandle = null
            dispatchRebuild(view)
          }, REBUILD_DEBOUNCE_MS)
        },
        destroy: () => {
          if (debounceHandle != null) window.clearTimeout(debounceHandle)
          if (mountDelayHandle != null) window.clearTimeout(mountDelayHandle)
        },
      }
    },
    props: {
      decorations(state: EditorState) {
        return this.getState(state)?.decos
      },
      handleClick(_view, _pos, event) {
        const target = event.target as HTMLElement | null
        if (!target) return false
        const el = target.closest('.tela-comment-anchor') as HTMLElement | null
        if (!el) return false
        const idAttr = el.getAttribute('data-comment-thread-id')
        if (!idAttr) return false
        const id = Number(idAttr)
        if (!Number.isFinite(id)) return false
        const callbacks = ctx.get(commentAnchorCallbacksCtx.key)
        callbacks?.onAnchorClick(id)
        // Return false so PM still places the caret normally. The user can
        // edit text inside a commented passage; the panel just opens beside.
        return false
      },
    },
  })
}

function dispatchRebuild(view: EditorView) {
  view.dispatch(view.state.tr.setMeta(COMMENT_ANCHOR_META, true))
}

function buildDecorations(
  doc: ProseNode,
  threads: CommentThread[] | null,
  callbacks: CommentAnchorCallbacks | null,
): DecorationSet {
  if (!threads || threads.length === 0) {
    callbacks?.onResolved(new Map())
    return DecorationSet.empty
  }
  // Single textBetween call — resolveAnchor is pure-string from here. Use
  // the exact projection capture uses (\n block separator); ANYTHING else
  // (textContent / space-separator) silently breaks tier-1 matching.
  const currentText = doc.textBetween(0, doc.content.size, '\n')
  const segments = buildTextSegments(doc)
  const decos: Decoration[] = []
  const resolutions = new Map<number, { from: number; to: number } | null>()

  for (const thread of threads) {
    const { root } = thread
    // Roots can be optimistic (negative id) — skip; the optimistic row shows
    // in the panel but the underline waits for server confirmation.
    if (root.id < 0) continue
    // Resolved threads are M8.5's scope — for M8.4 we still resolve so the
    // panel knows orphan state, but emit no decoration so the underline
    // disappears once a thread is marked resolved. M8.5 will replace this
    // with a muted underline gated by the filter toggle.
    if (root.resolved) continue
    if (!root.anchor_exact) {
      // Defensive — backend won't return a root without anchor fields. A
      // non-anchored legacy row should not crash the plugin.
      resolutions.set(root.id, null)
      continue
    }
    const anchor: CommentAnchor = {
      prefix: root.anchor_prefix ?? '',
      exact: root.anchor_exact,
      suffix: root.anchor_suffix ?? '',
    }
    const resolved = resolveAnchor(currentText, anchor)
    resolutions.set(root.id, resolved)
    if (!resolved) continue

    // Translate plain-text offsets (textBetween coordinates) back to PM
    // positions. ±1 drift at block boundaries — acknowledged in #70 and
    // absorbed by resolveAnchor's tier 2/3 fallbacks for capture-side; on
    // resolution we either land cleanly inside a text node or skip if the
    // range straddles a way the segment map can't subdivide.
    const pmFrom = plainOffsetToPm(segments, resolved.from)
    const pmTo = plainOffsetToPm(segments, resolved.to)
    if (pmFrom == null || pmTo == null || pmFrom >= pmTo) continue
    decos.push(
      Decoration.inline(pmFrom, pmTo, {
        class: 'tela-comment-anchor',
        'data-comment-thread-id': String(root.id),
      }),
    )
  }

  callbacks?.onResolved(resolutions)
  return DecorationSet.create(doc, decos)
}

// Segment table — one entry per contiguous run of plain-text characters that
// share a linear PM-position relationship. The map mirrors ProseMirror's own
// textBetween('\n') walker so plain-offset → PM-position translation stays
// in lockstep with the projection captureAnchor / resolveAnchor consume.
//
// Algorithm matches PM's textBetween semantics:
//   - text nodes append their content (linear PM mapping)
//   - inline leaf nodes append their leafText (hard_break → '\n'; image → '')
//   - block boundaries between emitted content append the separator ('\n')
//     and contribute no PM-position (the '\n' is virtual)
interface TextSegment {
  plainStart: number // inclusive
  plainEnd: number // exclusive
  pmStart: number // PM pos corresponding to plainStart
  // When true, this segment came from a leaf node (hard_break, image) whose
  // internal positions can't be subdivided — plain offsets inside it round
  // to plainStart (the leaf's leading PM position).
  isLeaf: boolean
}

function buildTextSegments(doc: ProseNode): TextSegment[] {
  const segs: TextSegment[] = []
  let plain = 0
  let first = true

  doc.nodesBetween(0, doc.content.size, (node, pos) => {
    if (node.isText) {
      const t = node.text ?? ''
      segs.push({
        plainStart: plain,
        plainEnd: plain + t.length,
        pmStart: pos,
        isLeaf: false,
      })
      plain += t.length
      first = false
      return false
    }
    if (node.isLeaf) {
      // hard_break or image — textBetween appends leafText if defined; for
      // hard_break PM uses '\n' by default; for image, nothing.
      const spec = node.type.spec as { leafText?: (n: ProseNode) => string }
      const leafText = spec.leafText
        ? spec.leafText(node)
        : node.type.name === 'hard_break'
          ? '\n'
          : ''
      if (leafText.length > 0) {
        segs.push({
          plainStart: plain,
          plainEnd: plain + leafText.length,
          pmStart: pos,
          isLeaf: true,
        })
        plain += leafText.length
        first = false
      }
      return false
    }
    if (!first && node.isBlock) {
      // Block boundary — emit a '\n' separator with no PM position.
      plain += 1
      first = true
    }
    return true
  })

  return segs
}

function plainOffsetToPm(
  segments: TextSegment[],
  plainOffset: number,
): number | null {
  // Linear scan. Segment count is bounded by inline text-node count; on a
  // typical page (~hundreds of nodes max) this is fine. If it ever becomes
  // a hotspot, binary-search on plainStart since segments are pre-sorted.
  for (const seg of segments) {
    if (plainOffset < seg.plainStart) {
      // Offset landed inside a block separator before this segment — the
      // closest in-bounds PM pos is the start of this segment.
      return seg.pmStart
    }
    if (plainOffset <= seg.plainEnd) {
      if (seg.isLeaf) {
        // Leaf segments can't be subdivided — round to the leaf's start.
        return seg.pmStart
      }
      return seg.pmStart + (plainOffset - seg.plainStart)
    }
  }
  // Past the end of all segments — fall back to the very last PM position
  // we know about, if any.
  if (segments.length === 0) return null
  const last = segments[segments.length - 1]
  return last.pmStart + (last.plainEnd - last.plainStart)
}
