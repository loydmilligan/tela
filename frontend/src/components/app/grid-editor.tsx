import { useCallback, useEffect, useMemo, useState } from 'react'
import * as Y from 'yjs'
import { normalize } from '@defterjs/core'
import { createEngine, FUNCTION_NAMES } from '@defterjs/formula'
import { DefterGrid } from '@defterjs/react'
import { useYText } from '@defterjs/yjs'
import '@defterjs/react/styles.css'
import { cn } from '../../lib/utils'
import { decodeSyncInit } from '../../lib/collab/encode'
import { useCollabSession } from '../../lib/collab/use-collab-session'
import '../../styles/defter-grid.css'

// GridEditor — the SHEET doc-type editor. A sheet's body is Defter markdown; the
// grid is a live projection of it. This mirrors MilkdownEditor's collab contract
// (defaultValue / onChange / collabPageId / readOnly) but binds a Y.Text instead
// of a Y.XmlFragment: the Y.Text *is* the canonical markdown, so "save" is just
// its .toString() — no serialize step. Collaboration rides tela's existing
// TelaProvider (the same /ws page room, which relays any Yjs update type-agnostic).
//
// A shared formula engine (dependency-free) is created once; `FUNCTION_NAMES` feeds
// formula autocomplete. Theme comes from the `.defter-shell` var mapping
// (styles/defter-grid.css) — the `theme` prop is deliberately left unset.

export interface GridEditorProps {
  defaultValue: string
  onChange: (text: string) => void
  onBlur?: () => void
  // null → no live session (viewer / solo draft): edit local state, host autosaves.
  collabPageId: number | null
  readOnly?: boolean
  pageId: number
  autoFocus?: boolean
  ariaLabel?: string
  className?: string
}

const engine = createEngine()

const GRID_PROPS = {
  engine,
  functions: FUNCTION_NAMES,
  toolbar: true,
  formulaBar: true,
  statusBar: true,
  sheetTabs: true,
  // Tile blank rows/cols past the data so the grid fills the (now full-height)
  // viewport like a real spreadsheet instead of leaving white space below the
  // last row. A generous fixed count covers common viewports; a true
  // fill-to-container behavior is a defter-side follow-up.
  extraRows: 40,
  extraCols: 14,
  // Marker on the grid's own `.defter-shell` root so the token bridge
  // (.defter-shell.tela-grid in defter-grid.css) maps onto tela tokens. See that
  // file's cascade note — it MUST land on defter's root, not just an ancestor.
  className: 'tela-grid',
} as const

export function GridEditor(props: GridEditorProps) {
  // Split so the collab hooks (useCollabSession/useYText) are never called
  // conditionally. A null page id (viewer/solo/draft) takes the local path.
  if (props.collabPageId == null) return <SoloGrid {...props} />
  return <CollabGrid key={props.collabPageId} {...props} />
}

// Local-state grid: no Yjs. Used for viewers (readOnly) and solo/draft edits.
function SoloGrid({ defaultValue, onChange, onBlur, readOnly, ariaLabel, className }: GridEditorProps) {
  const [text, setText] = useState(defaultValue)
  const onGridChange = useCallback(
    (next: string) => {
      setText(next)
      onChange(next)
    },
    [onChange],
  )
  return (
    <div className={cn('min-h-0 flex-1', className)} aria-label={ariaLabel} onBlur={onBlur}>
      <DefterGrid {...GRID_PROPS} text={text} onChange={readOnly ? undefined : onGridChange} readOnly={readOnly} />
    </div>
  )
}

// Live-collaborative grid. The Y.Text on tela's page Y.Doc holds the canonical
// Defter markdown; useYText mirrors it into `text` (reactive across local + remote
// edits) and gives a splice-back updater.
function CollabGrid({ defaultValue, onChange, onBlur, collabPageId, readOnly, ariaLabel, className }: GridEditorProps) {
  const { session, isLeaderRef } = useCollabSession(collabPageId)

  // A stable Y.Text handle for the lifetime of this session.
  const ytext = useMemo(() => session?.doc.getText('defter') ?? new Y.Doc().getText('defter'), [session])
  const [text, setText] = useYText(ytext)

  // Local edit: splice into the Y.Text (origin 'local'), then persist. Every peer
  // persists its OWN local edits — last-write-wins on the body PATCH.
  const onGridChange = useCallback(
    (next: string) => {
      setText(next)
      onChange(next)
    },
    [setText, onChange],
  )

  // Instant paint: apply the server's persisted Yjs state over REST so the grid
  // shows immediately instead of waiting for the WS sync-init. Applied with the
  // provider as origin so the update observer treats it as server content (and the
  // idempotent WS re-delivery is a no-op).
  useEffect(() => {
    if (collabPageId == null) return
    const collab = session
    if (!collab) return
    let cancelled = false
    void (async () => {
      try {
        const res = await fetch(`/api/pages/${collabPageId}/yjs`, { credentials: 'include' })
        if (!res.ok || cancelled) return
        const buf = new Uint8Array(await res.arrayBuffer())
        if (cancelled || buf.byteLength === 0 || collab.provider.isDestroyed()) return
        const { snapshot, updates } = decodeSyncInit(buf)
        if (snapshot) Y.applyUpdate(collab.doc, snapshot, collab.provider)
        for (const u of updates) Y.applyUpdate(collab.doc, u, collab.provider)
      } catch {
        // Best-effort — the WS sync-init still delivers the state.
      }
    })()
    return () => {
      cancelled = true
    }
  }, [collabPageId, session])

  // Empty-room seed: a brand-new sheet has no server Yjs state yet, and binding a
  // Y.Text does NOT push the body into it. On a confirmed-fresh room, seed the
  // Y.Text with the NORMALIZED body so the first collab edit is a minimal splice.
  useEffect(() => {
    const collab = session
    if (!collab) return
    if (!defaultValue || defaultValue.trim().length === 0) return
    let cancelled = false
    const unsub = collab.provider.onFirstSync(({ hadServerState }: { hadServerState: boolean }) => {
      if (cancelled || hadServerState) return
      if (ytext.length > 0) return
      ytext.insert(0, normalize(defaultValue))
    })
    return () => {
      cancelled = true
      unsub()
    }
  }, [session, ytext, defaultValue])

  // Remote-arrived content (origin === provider): the LEADER persists it so
  // pages.body doesn't lag the live doc when a non-leader peer edits. Local edits
  // (origin 'local') are excluded here — they persist via onGridChange above.
  useEffect(() => {
    const collab = session
    if (!collab || readOnly) return
    let firstSyncDone = false
    const unsubFirst = collab.provider.onFirstSync(() => {
      firstSyncDone = true
    })
    const onUpdate = (_update: Uint8Array, origin: unknown) => {
      if (origin !== collab.provider) return
      if (!firstSyncDone || !isLeaderRef.current) return
      onChange(ytext.toString())
    }
    collab.doc.on('update', onUpdate)
    return () => {
      unsubFirst()
      collab.doc.off('update', onUpdate)
    }
  }, [session, ytext, readOnly, onChange, isLeaderRef])

  return (
    <div className={cn('min-h-0 flex-1', className)} aria-label={ariaLabel} onBlur={onBlur}>
      <DefterGrid {...GRID_PROPS} text={text} onChange={readOnly ? undefined : onGridChange} readOnly={readOnly} />
    </div>
  )
}
