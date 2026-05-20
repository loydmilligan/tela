import { $ctx, $prose } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'

// M13.5 (#116) — modifier-click handling for editors. Inside a contenteditable
// surface the browser default for an <a> click is "place caret", not "navigate",
// and for a forced-open <details> (see detailsReadOnlyCtx in
// milkdown-collapsibles.ts) there's no closed state to toggle. We restore the
// Notion / Linear gesture: plain click = edit (PM's caret placement), ctrl-
// click (Linux/Windows) or cmd-click (macOS) = follow link / toggle
// collapsible. Both modifiers map to the same intent so the gesture works
// regardless of platform.
//
// Plugin shape:
// 1. `wikilinkNavigateCtx` — the host (MilkdownEditor in React) injects a
//    callback that resolves a `tela://page/{id}` to a router navigation.
//    Lookup needs the page's space_id (route is /spaces/$spaceId/pages/$pageId)
//    so the helper has to consult the TanStack Query cache at click time;
//    that lookup lives React-side via useQueryClient and is captured into a
//    ref so the plugin's ctx slice can read a stable function reference.
//    `null` short-circuits the handler (read-only / share / unmounted).
// 2. `modifierClickPlugin` — handleDOMEvents.click that fires before the
//    bubbled DOM listener (broken-wikilink). Returns true after handling so
//    PM doesn't run its default click flow. Calls event.preventDefault() but
//    does NOT call stopPropagation — the broken-wikilink listener guards
//    itself with a modifier-key check + event.defaultPrevented short-circuit
//    so coexistence is safe.
//
// Edge-case notes:
// - Link inside a <summary>: the closest('a') match wins (we test for it
//   first), so a modifier-click on a hyperlink in a summary follows the link
//   rather than toggling the disclosure. Mirrors GitHub's behaviour.
// - Empty/dummy hrefs (`#`, ``): ignored. Plain navigation would jump to the
//   page top; not useful as a "follow" intent.
// - tela:// page id that's not in the alive cache: the navigate callback
//   itself bails (no space_id resolvable). No-op; user can plain-click the
//   broken wikilink to invoke the existing new-page dialog.

export type WikilinkNavigateHandler = (pageId: number) => void

export const wikilinkNavigateCtx = $ctx<
  WikilinkNavigateHandler | null,
  'wikilinkNavigate'
>(null, 'wikilinkNavigate')

// Gates the whole plugin off in viewer (readOnly) and share modes. Set once
// at editor-build time from the React props (stable for the editor's
// lifetime — PageView keys by page id). Reading a ctx slice is cheaper than
// branching the plugin registration, and keeps the wire-up symmetric across
// the three modes.
export const modifierClickEnabledCtx = $ctx<boolean, 'modifierClickEnabled'>(
  false,
  'modifierClickEnabled',
)

const TELA_PAGE_HREF_PREFIX = 'tela://page/'

function parseTelaPageId(href: string): number | null {
  if (!href.startsWith(TELA_PAGE_HREF_PREFIX)) return null
  const tail = href.slice(TELA_PAGE_HREF_PREFIX.length)
  if (!/^\d+$/.test(tail)) return null
  return Number(tail)
}

// Returns the host <details class="tela-details"> for a clicked summary, or
// null if the summary isn't inside our managed disclosure widget (e.g. a
// stray raw HTML <details> the markdown serializer let through). We match
// on the class so we don't toggle native <details> outside our schema.
function findHostDetails(summary: Element): HTMLDetailsElement | null {
  const host = summary.closest('details.tela-details')
  return host instanceof HTMLDetailsElement ? host : null
}

export const modifierClickPlugin = $prose((ctx) => {
  return new Plugin({
    props: {
      handleDOMEvents: {
        click: (_view, event) => {
          if (!ctx.get(modifierClickEnabledCtx.key)) return false
          const target = event.target
          if (!(target instanceof Element)) return false
          const modifier = event.ctrlKey || event.metaKey

          const anchor = target.closest('a')
          if (anchor instanceof HTMLAnchorElement) {
            if (!modifier) return false
            const href = anchor.getAttribute('href') ?? ''
            if (href.length === 0 || href === '#') return false

            const telaPageId = parseTelaPageId(href)
            if (telaPageId != null) {
              const navigate = ctx.get(wikilinkNavigateCtx.key)
              if (!navigate) return false
              navigate(telaPageId)
              event.preventDefault()
              return true
            }

            if (/^https?:\/\//i.test(href)) {
              window.open(href, '_blank', 'noopener,noreferrer')
              event.preventDefault()
              return true
            }
            // Unknown scheme (mailto:, custom protocols, in-page anchors).
            // Fall through to native — same as plain click.
            return false
          }

          const summary = target.closest('summary')
          if (summary) {
            const host = findHostDetails(summary)
            if (!host) return false
            if (modifier) {
              // Toggle the wrapper's open state directly. detailsNodeView's
              // `ignoreMutation` blocks PM from reconciling the open attr,
              // so the toggle persists for the editor session (matches the
              // "transient open state" note in milkdown-collapsibles.ts).
              host.open = !host.open
              event.preventDefault()
              return true
            }
            // Plain click in editable mode: preserve the M13.4 force-open
            // invariant. We're in editable mode (modifierClickEnabled === true
            // means !readOnly && wikilinkMode !== 'share'), so the host was
            // mounted with `open=''` by detailsNodeView for caret-routing.
            // The browser's native summary-click toggle would close it,
            // breaking caret-routing into the body. Suppress the native
            // toggle but return false so PM still places the caret at the
            // click position — without the user feeling any movement.
            event.preventDefault()
            return false
          }

          return false
        },
      },
    },
  })
})
