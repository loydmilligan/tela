import { Suspense, lazy, useEffect, useState } from 'react'

// Same chunk as the real reader/editor (deduped by the module loader).
const MilkdownEditor = lazy(() =>
  import('./milkdown-editor').then((m) => ({ default: m.MilkdownEditor })),
)

// Once per page load: the warmup pays a session-wide cost, so a second mount
// (e.g. app layout + public index both rendering) must not re-run it.
let warmedThisSession = false

const WARM_DOC = '# \n\ntela'
const noop = () => {}

// Pre-pays the Milkdown editor's one-time *first-mount* cost — building 70+
// plugins, compiling the ProseMirror schema, registering refractor grammars,
// injecting the editor CSS (~400 ms, measured) — in the background during idle,
// so the user's FIRST real page open paints at warm speed (~70-140 ms) instead
// of stalling on the cold mount. Every later open was already fast; this closes
// the one remaining slow case.
//
// The throwaway instance is read-only, off-screen, and inert: no collab
// (collabPageId unset → no Y.Doc / websocket) and pageId 0 + no spaceId, which
// gates off every network-touching hook (image upload, wikilink resolve, mira
// paste). It unmounts itself once the cost is paid. Skipped on Save-Data / 2G.
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

    const ric =
      window.requestIdleCallback ??
      ((cb: () => void) => window.setTimeout(cb, 200))
    const handle = ric(() => setMount(true))
    return () => {
      ;(window.cancelIdleCallback ?? window.clearTimeout)(handle as number)
    }
  }, [])

  // Tear the throwaway view down once it's mounted+rendered (cost is paid).
  useEffect(() => {
    if (!mount) return
    const t = window.setTimeout(() => setMount(false), 3000)
    return () => window.clearTimeout(t)
  }, [mount])

  if (!mount) return null
  return (
    <div
      aria-hidden
      style={{
        position: 'fixed',
        left: -9999,
        top: -9999,
        width: 0,
        height: 0,
        overflow: 'hidden',
        opacity: 0,
        pointerEvents: 'none',
      }}
    >
      <Suspense fallback={null}>
        <MilkdownEditor defaultValue={WARM_DOC} onChange={noop} readOnly />
      </Suspense>
    </div>
  )
}
