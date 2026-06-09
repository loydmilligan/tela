import { Suspense, lazy, useEffect, useState } from 'react'

// Same chunk as the real reader/editor (deduped by the module loader).
const MilkdownEditor = lazy(() =>
  import('./milkdown-editor').then((m) => ({ default: m.MilkdownEditor })),
)

// Once per page load: the warmup pays a session-wide cost, so a second mount
// (e.g. app layout + public index both rendering) must not re-run it.
let warmedThisSession = false

// A representative body: the first real doc open is slow because each heavy
// node-view compiles/JITs on first render — refractor + the code-block view, the
// table view, callouts, lists. A trivial warmup body skips those paths and
// leaves the first real open slow (measured). Exercise the common ones here so
// the warmup actually pre-pays them.
const WARM_DOC = `# Warmup

Paragraph with **bold**, _italic_, \`code\`, and a [link](https://example.com).

- one
- two
- [ ] task

> [!NOTE]
> A callout.

\`\`\`js
const x = 1
function f(a) { return a + x }
\`\`\`

| A | B |
| - | - |
| 1 | 2 |
`
const noop = () => {}

// Pre-pays the Milkdown editor's one-time *first-mount* cost — building 70+
// plugins, compiling the ProseMirror schema, registering refractor grammars,
// injecting the editor CSS (~400 ms, measured) — in the background during idle,
// so the user's FIRST real page open paints at warm speed (~70-140 ms) instead
// of stalling on the cold mount. Every later open was already fast; this closes
// the one remaining slow case.
//
// The instance is off-screen and fully inert — it mounts in the same
// `wikilinkMode="share"` the public reader uses, which is the only mode that
// fires NO network: it skips the slash / wikilink / emoji-autocomplete / block
// plugins that would otherwise hit /api/pages/all (a 401 logged-out → global
// auth redirect to /login). No collab (collabPageId unset → no Y.Doc/websocket)
// and pageId 0 + no spaceId gate off the remaining hooks (image upload, mira
// paste). Skipped on Save-Data / 2G.
//
// It must be given a REAL size and stay mounted: a zero-size container never
// lays out, so ProseMirror's view never initialises and the mount cost is never
// actually paid (measured: a 0×0 warmup left the first real open just as slow).
// One inert hidden editor for the session is cheap; we keep it so the warm state
// persists rather than racing an unmount.
export function EditorWarmup() {
  const [mount, setMount] = useState(false)

  useEffect(() => {
    if (warmedThisSession) return
    const conn = (
      navigator as Navigator & {
        connection?: { saveData?: boolean; effectiveType?: string }
      }
    ).connection
    if (conn?.saveData || /(^|-)2g$/.test(conn?.effectiveType ?? '')) return
    warmedThisSession = true

    // rIC pays it during idle; the setTimeout is a hard fallback for browsers
    // (or busy pages) where idle never fires within a reasonable window.
    const ric =
      window.requestIdleCallback ??
      ((cb: () => void) => window.setTimeout(cb, 300))
    const handle = ric(() => setMount(true))
    const fallback = window.setTimeout(() => setMount(true), 1500)
    return () => {
      ;(window.cancelIdleCallback ?? window.clearTimeout)(handle as number)
      window.clearTimeout(fallback)
    }
  }, [])

  if (!mount) return null
  return (
    <div
      aria-hidden
      // Off-screen but real-sized + visible-to-layout, so the ProseMirror view
      // actually initialises (the whole point). pointer/tab inert.
      style={{
        position: 'fixed',
        left: '-10000px',
        top: 0,
        width: '780px',
        height: '600px',
        overflow: 'hidden',
        opacity: 0,
        pointerEvents: 'none',
      }}
    >
      <Suspense fallback={null}>
        <MilkdownEditor
          defaultValue={WARM_DOC}
          onChange={noop}
          readOnly
          wikilinkMode="share"
        />
      </Suspense>
    </div>
  )
}
