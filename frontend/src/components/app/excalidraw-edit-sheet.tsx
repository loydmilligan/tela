import { Suspense, lazy, useEffect, useRef, useState } from 'react'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '../ui/sheet'
import { api, ApiError } from '../../lib/api'
import type {
  DiagramCollaborator,
  DiagramSession,
  SceneElement,
} from '../../lib/collab/diagram-session'

// M13.3b — Excalidraw Edit Sheet.
//
// Full-viewport Sheet that wraps `@excalidraw/excalidraw@0.18.1`. The library
// (~290 KB gz total) is dyn-imported on Sheet open so it lands in its OWN
// lazy chunk (verified via `npm run build`); the view path (M13.3a) stays
// runtime-free.
//
// Save flow:
//   1. Read latest scene snapshot from `excalidrawAPI.getSceneElements()` +
//      `getAppState()` (or fall back to the last onChange snapshot if the API
//      ref hasn't bound yet — happens immediately after mount).
//   2. Whitelist appState fields to drop the un-serializable `collaborators`
//      Map + transient runtime fields (selection bounds, animation frame ids,
//      etc.). Keep only what round-trips into a fresh editor: theme,
//      viewBackgroundColor, gridSize.
//   3. Compute scene_hash = SHA256(JSON({elements, appState})) truncated to
//      32 lowercase hex chars (well within backend's `^[a-f0-9]{8,64}$`).
//   4. exportToBlob → arrayBuffer → base64 → PUT /api/pages/{id}/diagrams
//      with {scene_hash, png_base64}.
//   5. Compose sceneJSON for markdown: include scene_hash + alt_text inline
//      so the fence round-trips into the M13.3a view-mode renderer.
//   6. onSave({sceneHash, altText, sceneJSON}) — host (PageView) dispatches
//      the ProseMirror tx that updates the atom node's attrs.
//
// On any save failure the Sheet stays open so the user doesn't lose work.

interface DiagramPayload {
  sceneHash: string
  altText: string
  sceneJSON: string
  diagramId: string
}

export interface ExcalidrawEditSheetProps {
  open: boolean
  onOpenChange: (next: boolean) => void
  pageId: number
  initialJSON: string
  initialAltText: string
  // Stable diagram id from the atom; '' for legacy diagrams (we stamp a fresh
  // one on save so future sessions key on it).
  initialDiagramId: string
  onSave: (next: DiagramPayload) => void | Promise<void>
  // SPIKE — live multiplayer. When present (collab mode), the canvas streams
  // its scene/pointer over the ephemeral channel and applies remote peers'
  // edits. Null in single-user/share/view mode → today's solo behaviour.
  session?: DiagramSession | null
}

// Editor map ours → Excalidraw. `dark` → dark; `light` + `warm` → light.
// Excalidraw v0.18 supports prop-driven theme switching (the `theme` prop
// updates the canvas chrome on re-render without losing scene state), so we
// track `data-theme` on <html> via a MutationObserver and feed the live value
// into the component — see useExcalidrawTheme below.
function detectExcalidrawTheme(): 'light' | 'dark' {
  if (typeof document === 'undefined') return 'light'
  const t = document.documentElement.getAttribute('data-theme')
  return t === 'dark' ? 'dark' : 'light'
}

function useExcalidrawTheme(): 'light' | 'dark' {
  const [theme, setTheme] = useState<'light' | 'dark'>(detectExcalidrawTheme)
  useEffect(() => {
    if (typeof document === 'undefined') return
    const root = document.documentElement
    const observer = new MutationObserver(() => {
      const next = root.getAttribute('data-theme') === 'dark' ? 'dark' : 'light'
      setTheme((prev) => (prev === next ? prev : next))
    })
    observer.observe(root, { attributes: true, attributeFilter: ['data-theme'] })
    return () => observer.disconnect()
  }, [])
  return theme
}

// Dyn-imported wrapper. Two reasons to load BOTH the component and the export
// utility under one lazy boundary:
//   - Single network round-trip + single chunk on Sheet first-open.
//   - `exportToBlob` is only ever called inside this component, so colocating
//     it in the same lazy module keeps the main bundle clean.
const ExcalidrawCanvas = lazy(async () => {
  const mod = await import('@excalidraw/excalidraw')
  // Library ships its own stylesheet — must be imported for the canvas chrome
  // to render. Side-effect import; Vite handles the css → blob inline path so
  // it lands in the same chunk-extracted CSS asset list, not the main CSS.
  await import('@excalidraw/excalidraw/index.css')
  return { default: buildCanvas(mod) }
})

interface ExcalidrawModule {
  Excalidraw: React.ComponentType<ExcalidrawComponentProps>
  exportToBlob: (opts: ExportOpts) => Promise<Blob>
  // Per-element last-writer-wins merge (by version). Used to fold a remote
  // peer's scene delta into our local scene — Excalidraw's own convergence,
  // so the spike writes no merge logic.
  reconcileElements: (
    local: readonly unknown[],
    remote: readonly unknown[],
    localAppState: Record<string, unknown>,
  ) => readonly unknown[]
}

interface ExportOpts {
  elements: readonly unknown[]
  appState?: Record<string, unknown>
  files: Record<string, unknown> | null
  mimeType?: string
  exportPadding?: number
  getDimensions?: (
    w: number,
    h: number,
  ) => { width: number; height: number; scale?: number }
}

interface ExcalidrawImperativeAPI {
  getSceneElements: () => readonly unknown[]
  getAppState: () => Record<string, unknown>
  getFiles: () => Record<string, unknown>
  updateScene: (scene: {
    elements?: readonly unknown[]
    collaborators?: Map<string, unknown>
  }) => void
}

interface ExcalidrawComponentProps {
  initialData?: { elements?: readonly unknown[]; appState?: Record<string, unknown> } | null
  onChange?: (
    elements: readonly unknown[],
    appState: Record<string, unknown>,
    files: Record<string, unknown>,
  ) => void
  onPointerUpdate?: (payload: {
    pointer: { x: number; y: number }
  }) => void
  excalidrawAPI?: (api: ExcalidrawImperativeAPI) => void
  isCollaborating?: boolean
  theme?: 'light' | 'dark'
}

interface CanvasProps {
  initialData: { elements: readonly unknown[]; appState: Record<string, unknown> } | null
  theme: 'light' | 'dark'
  apiRef: React.MutableRefObject<ExcalidrawImperativeAPI | null>
  session?: DiagramSession | null
  onSnapshot: (snap: {
    elements: readonly unknown[]
    appState: Record<string, unknown>
    files: Record<string, unknown>
  }) => void
}

// Closure over the dyn-imported module so the canvas + exportToBlob share one
// import (avoids two parallel imports racing each other on first open).
let cachedModule: ExcalidrawModule | null = null

// Map a colorIdx (0..7) to a concrete cursor color — Excalidraw's collaborator
// API needs a hex, not a CSS token. SPIKE palette; Phase 1 derives these from
// the --collab-cursor-{1..8} tokens.
// Persist the converged scene after this long without an edit, in a live
// session — durability without a manual Save; a crash loses at most this much.
const CHECKPOINT_IDLE_MS = 2500

const CURSOR_HEX = [
  '#e8590c', '#1971c2', '#2f9e44', '#9c36b5',
  '#c2255c', '#0c8599', '#e67700', '#5f3dc4',
] as const

function buildCanvas(mod: unknown) {
  const m = mod as ExcalidrawModule
  cachedModule = m
  return function Canvas({
    initialData,
    theme,
    apiRef,
    session,
    onSnapshot,
  }: CanvasProps) {
    const ExcalidrawComp = m.Excalidraw

    // Subscribe to remote peers: fold their scene deltas into ours via
    // reconcileElements, and render their cursors via updateScene's
    // collaborators map. Transport-only — DiagramSession knows no Excalidraw.
    useEffect(() => {
      if (!session) return
      const unsubScene = session.onRemoteScene((remote) => {
        const api = apiRef.current
        if (!api) return
        const local = api.getSceneElements()
        const merged = m.reconcileElements(local, remote, api.getAppState())
        api.updateScene({ elements: merged })
      })
      const unsubCollab = session.onCollaborators((collab) => {
        const api = apiRef.current
        if (!api) return
        api.updateScene({ collaborators: toExcalidrawCollaborators(collab) })
      })
      return () => {
        unsubScene()
        unsubCollab()
      }
    }, [session, apiRef])

    return (
      <ExcalidrawComp
        initialData={initialData}
        theme={theme}
        isCollaborating={!!session}
        excalidrawAPI={(api) => {
          apiRef.current = api
        }}
        onChange={(elements, appState, files) => {
          onSnapshot({ elements, appState, files })
          session?.pushScene(elements as SceneElement[])
        }}
        onPointerUpdate={({ pointer }) => {
          session?.pushPointer(pointer.x, pointer.y)
        }}
      />
    )
  }
}

// Adapt DiagramSession's collaborator map into the shape Excalidraw's
// updateScene({ collaborators }) expects (keyed by socketId-ish string).
function toExcalidrawCollaborators(
  collab: Map<string, DiagramCollaborator>,
): Map<string, unknown> {
  const out = new Map<string, unknown>()
  for (const [k, c] of collab) {
    out.set(k, {
      pointer: c.pointer,
      username: c.username,
      color: { background: CURSOR_HEX[c.colorIdx % 8], stroke: '#ffffff' },
    })
  }
  return out
}

// Strip transient runtime fields off appState so the JSON round-trips cleanly
// through markdown without ballooning. The Map-typed `collaborators` field
// can't be JSON-stringified at all; the cursor/selection/animation fields
// are per-session ephemeral and only confuse byte-for-byte diff readers.
function sanitizeAppState(
  appState: Record<string, unknown>,
): Record<string, unknown> {
  const allow = [
    'viewBackgroundColor',
    'gridSize',
    'theme',
    'currentItemFontFamily',
    'currentItemStrokeColor',
    'currentItemBackgroundColor',
    'currentItemFillStyle',
    'currentItemStrokeWidth',
    'currentItemStrokeStyle',
    'currentItemRoughness',
    'currentItemOpacity',
    'currentItemFontSize',
    'currentItemTextAlign',
    'currentItemStartArrowhead',
    'currentItemEndArrowhead',
  ] as const
  const out: Record<string, unknown> = {}
  for (const key of allow) {
    if (key in appState) out[key] = appState[key]
  }
  return out
}

async function computeSceneHash(canonical: string): Promise<string> {
  const enc = new TextEncoder().encode(canonical)
  const buf = await crypto.subtle.digest('SHA-256', enc)
  const bytes = new Uint8Array(buf)
  let hex = ''
  for (const b of bytes) hex += b.toString(16).padStart(2, '0')
  return hex.slice(0, 32)
}

function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error ?? new Error('FileReader failed'))
    reader.onload = () => {
      const result = reader.result
      if (typeof result !== 'string') {
        reject(new Error('FileReader did not return a string'))
        return
      }
      const comma = result.indexOf(',')
      resolve(comma >= 0 ? result.slice(comma + 1) : result)
    }
    reader.readAsDataURL(blob)
  })
}

interface UploadResponse {
  id: number
  page_id: number
  scene_hash: string
  byte_size: number
  url: string
}

// The commit-dedupe key = scene content hash + alt text. Re-committing an
// unchanged diagram (e.g. a checkpoint right after open, or a close with no
// edits) is skipped against this. Alt text is folded in because it's persisted
// but isn't part of scene_hash.
function commitKey(sceneHash: string, altText: string): string {
  return `${sceneHash} ${altText}`
}

function parseInitialSceneHash(raw: string): string {
  if (!raw) return ''
  try {
    const parsed = JSON.parse(raw) as { scene_hash?: unknown }
    return typeof parsed.scene_hash === 'string' ? parsed.scene_hash : ''
  } catch {
    return ''
  }
}

function parseInitialData(
  raw: string,
): { elements: readonly unknown[]; appState: Record<string, unknown> } | null {
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as {
      elements?: unknown
      appState?: unknown
    }
    return {
      elements: Array.isArray(parsed.elements) ? parsed.elements : [],
      appState:
        parsed.appState && typeof parsed.appState === 'object'
          ? (parsed.appState as Record<string, unknown>)
          : {},
    }
  } catch {
    return null
  }
}

export function ExcalidrawEditSheet({
  open,
  onOpenChange,
  pageId,
  initialJSON,
  initialAltText,
  initialDiagramId,
  onSave,
  session,
}: ExcalidrawEditSheetProps) {
  const [altText, setAltText] = useState(initialAltText)
  const [status, setStatus] = useState<'idle' | 'saving' | 'error'>('idle')
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const apiRef = useRef<ExcalidrawImperativeAPI | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  // Stash latest snapshot from onChange — fallback for environments where the
  // imperative API ref doesn't bind in time (very fast Save clicks on slow
  // hardware). The api ref is the preferred source when set.
  const latestSnapshotRef = useRef<{
    elements: readonly unknown[]
    appState: Record<string, unknown>
    files: Record<string, unknown>
  } | null>(null)
  // Dedupe key of the last persisted scene+altText, so checkpoints/closes don't
  // re-upload an unchanged diagram. Seeded from the opened diagram on each open.
  const lastCommittedKeyRef = useRef('')
  // Guards against two commits exporting/uploading at once (a silent checkpoint
  // racing an explicit Save).
  const committingRef = useRef(false)
  // Idle-debounce timer for live-collab checkpoints.
  const checkpointTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const initialData = parseInitialData(initialJSON)
  const theme = useExcalidrawTheme()

  // Reset transient state every time the sheet (re)opens. One-shot effect
  // tied to `open`: the eslint rule doesn't model the "edge-triggered reset"
  // pattern, which is the canonical way to recycle a controlled-Sheet's
  // internal state on each open without keying the whole component.
  useEffect(() => {
    if (!open) {
      if (checkpointTimerRef.current != null) {
        clearTimeout(checkpointTimerRef.current)
        checkpointTimerRef.current = null
      }
      return
    }
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setAltText(initialAltText)
    setStatus('idle')
    setErrorMessage(null)
    latestSnapshotRef.current = null
    apiRef.current = null
    committingRef.current = false
    lastCommittedKeyRef.current = commitKey(
      parseInitialSceneHash(initialJSON),
      initialAltText,
    )
  }, [open, initialAltText, initialJSON])

  // Pointer-offset fix — defer mounting Excalidraw until the Sheet's slide-in
  // settles. The Sheet enters via a `transform: translateX` animation (see
  // .tela-sheet-content--right in index.css). Excalidraw measures its container
  // offset/size on mount and, for a SAVED drawing, immediately runs
  // scrollToContent to fit the existing elements. If it mounts mid-slide (every
  // open after the first, once the lazy chunk is cached — i.e. reopening saved
  // diagrams) those measurements are taken against a still-translated container,
  // so the pointer→scene mapping is shifted and drawing lands away from the
  // cursor. A fresh/empty diagram has no elements to fit, which is why the bug
  // only shows on saved ones. We can't patch this after the fact (re-measuring
  // the offset doesn't re-run the content fit), so we wait for the slide's
  // `animationend` (with a duration-based fallback) before rendering the canvas
  // at all — it then mounts into a stable, final-position container.
  const [settled, setSettled] = useState(false)
  useEffect(() => {
    if (!open) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- intentional reset on close
      setSettled(false)
      return
    }
    let done = false
    const settle = () => {
      if (done) return
      done = true
      setSettled(true)
    }
    const content = containerRef.current?.closest('.tela-sheet-content')
    content?.addEventListener('animationend', settle)
    // Fallback if animationend never fires (animation interrupted/absent) —
    // comfortably past --duration-base.
    const fallback = window.setTimeout(settle, 450)
    return () => {
      content?.removeEventListener('animationend', settle)
      window.clearTimeout(fallback)
    }
  }, [open])

  // The SINGLE commit path — used by the explicit Save, by close-in-session,
  // and by idle checkpoints. Exports the PNG, uploads it, and persists the
  // scene into the atom via onSave (the editor force-writes pages.body so the
  // drawing reaches the saved markdown regardless of collab leadership). Dedupes
  // against the last committed scene so redundant checkpoints/closes are cheap.
  //   - close:  close the sheet after a successful (or no-op) commit.
  //   - silent: a background checkpoint — no status UI, swallow errors (the
  //             next checkpoint or the close-commit retries).
  async function commitScene(opts: {
    close: boolean
    silent?: boolean
  }): Promise<void> {
    if (committingRef.current) {
      // A commit is already exporting (likely a silent checkpoint of the same
      // scene). Don't double-export; just honor a close request — the in-flight
      // commit persists the scene.
      if (opts.close) onOpenChange(false)
      return
    }
    committingRef.current = true
    if (!opts.silent) {
      setStatus('saving')
      setErrorMessage(null)
    }
    try {
      // Prefer the imperative API for the freshest snapshot; fall back to the
      // last onChange payload otherwise.
      const api = apiRef.current
      const snap = api
        ? {
            elements: api.getSceneElements(),
            appState: api.getAppState(),
            files: api.getFiles(),
          }
        : latestSnapshotRef.current
      const elements = snap?.elements ?? []
      const appState = sanitizeAppState(snap?.appState ?? {})
      const files = snap?.files ?? null

      const canonical = JSON.stringify({ elements, appState })
      const sceneHash = await computeSceneHash(canonical)

      // Nothing changed since the last persist → skip the upload + write.
      if (commitKey(sceneHash, altText) === lastCommittedKeyRef.current) {
        if (opts.close) onOpenChange(false)
        return
      }

      const mod = cachedModule
      if (!mod) {
        // Should never happen — the Save button is only rendered after the
        // canvas Suspense boundary resolves.
        throw new Error('Excalidraw module not loaded')
      }
      const blob = await mod.exportToBlob({
        elements,
        appState,
        files,
        mimeType: 'image/png',
        exportPadding: 16,
      })
      const png_base64 = await blobToBase64(blob)

      await uploadDiagram(pageId, { scene_hash: sceneHash, png_base64 })

      // Stamp a stable diagram id if the atom lacks one (legacy diagram) so
      // future live-collab sessions key on it. crypto.randomUUID() inline (not
      // imported from the editor module) keeps this lazy chunk decoupled.
      const diagramId = initialDiagramId || crypto.randomUUID()

      // Compose markdown sceneJSON: full scene + hash + alt-text + stable id so
      // external consumers (other tela instances, plain markdown viewers) can
      // locate the PNG without a separate sidecar and the id round-trips.
      const sceneJSON = JSON.stringify({
        elements,
        appState,
        scene_hash: sceneHash,
        alt_text: altText,
        diagram_id: diagramId,
      })

      await onSave({ sceneHash, altText, sceneJSON, diagramId })
      lastCommittedKeyRef.current = commitKey(sceneHash, altText)
      if (opts.close) onOpenChange(false)
    } catch (err) {
      if (!opts.silent) {
        setStatus('error')
        setErrorMessage(diagramErrorMessage(err))
      }
    } finally {
      committingRef.current = false
    }
  }

  // Always call the latest commitScene from the (stale-closure-prone) checkpoint
  // timer. Updated in an effect (ref writes during render are disallowed).
  const commitRef = useRef(commitScene)
  useEffect(() => {
    commitRef.current = commitScene
  })

  // Idle checkpoint: while a live-collab session is open, persist the converged
  // scene after a short pause in edits so mid-session work is durable without a
  // manual Save (and a crash loses at most the last few seconds). Only the
  // checkpoint leader writes, so N open peers don't all upload the same PNG.
  // Re-armed on every onChange (local OR remote, since updateScene re-fires it).
  function scheduleCheckpoint(): void {
    if (!session) return
    if (checkpointTimerRef.current != null) clearTimeout(checkpointTimerRef.current)
    checkpointTimerRef.current = setTimeout(() => {
      checkpointTimerRef.current = null
      if (!session.isCheckpointLeader()) return
      void commitRef.current({ close: false, silent: true })
    }, CHECKPOINT_IDLE_MS)
  }

  // Close intent (Cancel button, Esc, overlay click). In a live-collab session
  // there is no "discard" — your edits are already shared — so closing commits
  // a final time. Solo mode keeps the classic discard-on-cancel behaviour.
  function handleClose(): void {
    if (session) {
      void commitScene({ close: true })
    } else {
      onOpenChange(false)
    }
  }

  function handleOpenChange(next: boolean): void {
    if (next) return // controlled; never auto-opens
    handleClose()
  }

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent
        side="right"
        className="!w-screen sm:!max-w-none flex flex-col"
        onOpenAutoFocus={(e) => e.preventDefault()}
      >
        <SheetHeader>
          <SheetTitle>Edit diagram</SheetTitle>
          <SheetDescription>
            {session
              ? 'Changes sync live and save automatically.'
              : 'Draw your diagram, optionally add alt text, then Save to embed it in the page.'}
          </SheetDescription>
        </SheetHeader>

        <SheetBody className="p-0 min-h-0 flex-1">
          <div ref={containerRef} className="h-full w-full">
            {/* Hold the canvas back until the slide-in settles (see `settled`
                above) so Excalidraw measures the final container position. */}
            {settled ? (
              <Suspense fallback={<CanvasFallback />}>
                <ExcalidrawCanvas
                  initialData={initialData}
                  theme={theme}
                  apiRef={apiRef}
                  session={session}
                  onSnapshot={(snap) => {
                    latestSnapshotRef.current = snap
                    scheduleCheckpoint()
                  }}
                />
              </Suspense>
            ) : (
              <CanvasFallback />
            )}
          </div>
        </SheetBody>

        <SheetFooter className="flex-col items-stretch gap-[var(--space-2)] sm:flex-row sm:items-center">
          <Input
            value={altText}
            onChange={(e) => setAltText(e.target.value)}
            placeholder="Add alt text? (Helps screen readers and broken-image fallback)"
            aria-label="Diagram alt text"
            className="flex-1"
          />
          {status === 'error' && errorMessage ? (
            <p
              role="alert"
              className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
            >
              {errorMessage}
            </p>
          ) : null}
          <div className="flex items-center justify-end gap-[var(--space-2)]">
            <Button
              type="button"
              variant="ghost"
              onClick={handleClose}
              disabled={status === 'saving'}
            >
              {session ? 'Close' : 'Cancel'}
            </Button>
            <Button
              type="button"
              variant="primary"
              onClick={() => void commitScene({ close: true })}
              disabled={status === 'saving'}
            >
              {status === 'saving' ? 'Saving…' : session ? 'Save & close' : 'Save'}
            </Button>
          </div>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

function CanvasFallback() {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label="Loading diagram editor"
      className="flex-1 min-h-0 bg-[var(--surface-2)]"
    />
  )
}

async function uploadDiagram(
  pageId: number,
  body: { scene_hash: string; png_base64: string },
): Promise<UploadResponse> {
  return api<UploadResponse>(`/api/pages/${pageId}/diagrams`, {
    method: 'PUT',
    body: JSON.stringify(body),
  })
}

function diagramErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 413) return 'Diagram too large — try simplifying it.'
    if (err.status === 400) return 'Could not save diagram.'
    if (err.status === 401) return 'Session expired — please log in again.'
    return err.message || 'Could not save diagram.'
  }
  return err instanceof Error ? err.message : 'Could not save diagram.'
}
