import { useMemo, useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { expect, waitFor, userEvent } from 'storybook/test'
import * as Y from 'yjs'
import { Awareness, removeAwarenessStates } from 'y-protocols/awareness'
import { MilkdownEditor } from './milkdown-editor'
import type { CollabProviderFactory } from '../../lib/collab/use-collab-session'

// Save-continuity net for the collab editor, born from the page-1257 data
// loss: the editor kept accepting edits (Yjs converged fine) while every
// body save was silently gated off, so "Done" rendered a stale pages.body.
// Two invariants pinned here:
//   1. onChange keeps tracking through a paste-URL→unfurl link-list flow
//      (the exact authoring flow that hit the bug).
//   2. onChange keeps tracking after the local awareness entry is removed
//      mid-session (what pagehide does when the ws survives the restore) —
//      leader election must still count self as a candidate.

const meta = {
  title: 'App/Milkdown Save Continuity',
  parameters: { layout: 'fullscreen' },
} satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

const URLS: Array<{ u: string; title: string | null }> = [
  {
    u: 'https://www.collegetransitions.com/dataverse/act-sat-testing-policies/',
    title: 'Standardized Testing Policies ACT/SAT – 2025-2026',
  },
  {
    u: 'https://nces.ed.gov/ipeds/datacenter/DataFiles.aspx?gotoReportId=7&fromIpeds=true&sid=cffd6544&rtid=7',
    title: null, // unfurl fails → plain autolink
  },
  {
    u: 'https://docs.google.com/spreadsheets/d/e/2PACX-1vR/pubhtml?gid=2030538896&single=true',
    title: 'SAT & ACT Policies and Ranges - Google Drive',
  },
]

// Offline TelaProvider stand-in (mirrors the scenarios stories): real Y.Doc +
// Awareness so y-prosemirror binds and leader election runs. Exposes the
// awareness so tests can knock the local entry out mid-session.
const liveAwareness: { current: Awareness | null } = { current: null }
function fakeCollabProviderFactory(): CollabProviderFactory {
  return () => {
    const doc = new Y.Doc()
    const awareness = new Awareness(doc)
    liveAwareness.current = awareness
    let destroyed = false
    const provider = {
      doc,
      awareness,
      getStatus: () => 'connected',
      onStatus: () => () => {},
      onFirstSync: (fn: (i: { hadServerState: boolean }) => void) => {
        fn({ hadServerState: false })
        return () => {}
      },
      isDestroyed: () => destroyed,
      destroy: () => {
        destroyed = true
        const obs = (
          awareness as unknown as { _observers?: Map<string, unknown> }
        )._observers
        if (obs && typeof obs.clear === 'function') obs.clear()
        awareness.destroy()
      },
    }
    return {
      doc,
      provider: provider as unknown as ReturnType<CollabProviderFactory>['provider'],
    }
  }
}

function Harness({ id, collab = false }: { id: string; collab?: boolean }) {
  const [out, setOut] = useState('')
  const qc = useMemo(
    () => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    [],
  )
  const factory = useMemo(
    () => (collab ? fakeCollabProviderFactory() : undefined),
    [collab],
  )
  return (
    <QueryClientProvider client={qc}>
      <div style={{ padding: 16 }} data-testid={`case-${id}`}>
        <MilkdownEditor
          defaultValue={''}
          onChange={(md) => setOut(md)}
          collabPageId={collab ? 4242 : null}
          collabProviderFactory={factory}
          ariaLabel={`editor-${id}`}
        />
        <pre data-testid={`out-${id}`} style={{ display: 'none' }}>
          {out}
        </pre>
      </div>
    </QueryClientProvider>
  )
}

function stubUnfurl() {
  const realFetch = window.fetch.bind(window)
  window.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const href =
      typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (href.includes('/api/unfurl')) {
      const target = decodeURIComponent(href.split('url=')[1] ?? '')
      const hit = URLS.find((x) => target.startsWith(x.u.slice(0, 40)))
      if (hit?.title == null) return new Response('nope', { status: 502 })
      return new Response(JSON.stringify({ title: hit.title }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      })
    }
    return realFetch(input as RequestInfo, init)
  }) as typeof window.fetch
  return () => {
    window.fetch = realFetch
  }
}

function pasteText(pm: HTMLElement, text: string) {
  const dt = new DataTransfer()
  dt.setData('text/plain', text)
  pm.dispatchEvent(
    new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }),
  )
}

async function getEditable(canvasElement: HTMLElement, id: string) {
  let pm: HTMLElement | null = null
  await waitFor(
    () => {
      pm = canvasElement.querySelector<HTMLElement>(
        `[data-testid="case-${id}"] .ProseMirror[contenteditable]`,
      )
      expect(pm).not.toBeNull()
    },
    { timeout: 15000 },
  )
  return pm!
}

function outOf(canvasElement: HTMLElement, id: string) {
  return (
    canvasElement.querySelector(`[data-testid="out-${id}"]`)?.textContent ?? ''
  )
}

// Mara's authoring flow: bullet list, paste bare URLs one at a time, each
// unfurled to a titled link — then prove onChange still tracks at the end.
async function runUnfurlFlow(canvasElement: HTMLElement, id: string) {
  const restore = stubUnfurl()
  try {
    const pm = await getEditable(canvasElement, id)
    await userEvent.click(pm)
    await userEvent.keyboard('- ')
    for (const { u } of URLS) {
      pasteText(pm, u)
      await new Promise((r) => setTimeout(r, 120))
      await userEvent.keyboard('{Enter}')
    }
    await new Promise((r) => setTimeout(r, 400))
    await userEvent.keyboard('ZZSENTINELZZ')
    await waitFor(
      () => expect(outOf(canvasElement, id)).toContain('ZZSENTINELZZ'),
      { timeout: 6000 },
    )
    const out = outOf(canvasElement, id)
    for (const { u } of URLS) expect(out).toContain(u.split('?')[0])
  } finally {
    restore()
  }
}

export const UnfurlLinkListSolo: Story = {
  render: () => <Harness id="solo" />,
  play: async ({ canvasElement }) => runUnfurlFlow(canvasElement, 'solo'),
}

export const UnfurlLinkListCollab: Story = {
  render: () => <Harness id="collab" collab />,
  play: async ({ canvasElement }) => runUnfurlFlow(canvasElement, 'collab'),
}

// The regression: knock the local awareness entry out mid-session (exactly
// what TelaProvider's pagehide handler does; when the ws survives the restore
// nothing re-seeds it) and prove edits STILL reach onChange. Before the
// computeIsLeader fix this went save-dead: typing worked, onChange stayed
// frozen, and Done persisted a stale body.
export const CollabSaveSurvivesAwarenessDrop: Story = {
  render: () => <Harness id="drop" collab />,
  play: async ({ canvasElement }) => {
    const pm = await getEditable(canvasElement, 'drop')
    await userEvent.click(pm)
    await userEvent.keyboard('before drop')
    await waitFor(
      () => expect(outOf(canvasElement, 'drop')).toContain('before drop'),
      { timeout: 6000 },
    )
    const awareness = liveAwareness.current
    expect(awareness).not.toBeNull()
    removeAwarenessStates(awareness!, [awareness!.clientID], 'test-pagehide')
    expect(awareness!.getLocalState()).toBeNull()
    await userEvent.keyboard(' after drop')
    await waitFor(
      () => expect(outOf(canvasElement, 'drop')).toContain('after drop'),
      { timeout: 6000 },
    )
  },
}
