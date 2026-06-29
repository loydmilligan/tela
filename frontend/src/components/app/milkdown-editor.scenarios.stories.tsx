import { useMemo, useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { expect, userEvent, waitFor, within } from 'storybook/test'
import { MilkdownEditor } from './milkdown-editor'

// Behavioural tests that drive the REAL editor (solo / non-collab mode,
// collabPageId=null) end to end: markdown in → ProseMirror render → user edit →
// markdown out. The block insert mechanics are unit-tested in
// lib/milkdown/insert-block.test.ts; these stories are the integration net the
// unit tests can't give — that the slash menu, schemas, node views and the
// hand-written per-block toMarkdown serializers actually wire together. This is
// the first interaction-test harness in the repo; reuse it for editor work.

function Harness({ defaultValue = '' }: { defaultValue?: string }) {
  const qc = useMemo(
    () => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    [],
  )
  const [md, setMd] = useState(defaultValue)
  return (
    <QueryClientProvider client={qc}>
      <div style={{ padding: 16, maxWidth: '48rem' }}>
        <MilkdownEditor
          defaultValue={defaultValue}
          onChange={setMd}
          collabPageId={null}
          ariaLabel="Test editor"
        />
        <pre data-testid="md-out" style={{ display: 'none' }}>
          {md}
        </pre>
      </div>
    </QueryClientProvider>
  )
}

const meta: Meta<typeof Harness> = {
  title: 'App/Milkdown Editor Scenarios',
  component: Harness,
  parameters: { layout: 'fullscreen' },
}
export default meta

type Story = StoryObj<typeof Harness>

// Wait for the ProseMirror editable to finish its async mount.
async function getEditable(canvasEl: HTMLElement): Promise<HTMLElement> {
  let pm: HTMLElement | null = null
  await waitFor(
    () => {
      pm = canvasEl.querySelector<HTMLElement>('.ProseMirror[contenteditable]')
      expect(pm).not.toBeNull()
    },
    { timeout: 8000 },
  )
  return pm as unknown as HTMLElement
}

// ── Scenario 1: parse + render ──────────────────────────────────────────────
export const RendersRichBlocks: Story = {
  args: {
    defaultValue: [
      '# Heading one',
      '',
      '> [!NOTE]',
      '> An existing note.',
      '',
      '- one',
      '- two',
    ].join('\n'),
  },
  play: async ({ canvasElement }) => {
    const pm = await getEditable(canvasElement)
    await waitFor(() => {
      expect(pm.querySelector('.tela-callout')).not.toBeNull()
      expect(pm.querySelector('h1')).not.toBeNull()
      expect(pm.querySelector('ul')).not.toBeNull()
    })
    expect(pm.querySelector('h1')?.textContent).toContain('Heading one')
  },
}

// ── Scenario 2: slash-insert a callout, then type into its body ──────────────
export const SlashInsertCallout: Story = {
  args: { defaultValue: '' },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement)
    const pm = await getEditable(canvasElement)

    await userEvent.click(pm)
    await userEvent.keyboard('/callout')
    await waitFor(() => {
      expect(document.querySelector('.tela-slash-menu')).not.toBeNull()
    })
    await userEvent.keyboard('{Enter}')

    await waitFor(() => {
      expect(pm.querySelector('.tela-callout')).not.toBeNull()
    })
    await userEvent.keyboard('Body text')
    await waitFor(() => {
      expect(pm.querySelector('.tela-callout')?.textContent).toContain(
        'Body text',
      )
    })
    await waitFor(() => {
      const out = canvas.getByTestId('md-out').textContent ?? ''
      expect(out).toContain('[!NOTE]')
      expect(out).toContain('Body text')
    })
  },
}

// ── Scenario 3: round-trip fidelity across every block ──────────────────────
// Mount canonical markdown for each block, force the editor to re-serialize, and
// assert the structure survives parse → ProseMirror → serialize. The toMarkdown
// handlers are hand-written per block, so this is where drops/mangling hide.
// Each case lists substrings that MUST and MUST-NOT appear; all failures are
// collected into one report instead of aborting on the first.

interface RoundTripCase {
  id: string
  md: string
  contains: string[]
  notContains?: string[]
}

// Canonical block markdown (mirrors blocks-manifest.json).
const CORE_CASES: RoundTripCase[] = [
  {
    id: 'callout',
    md: '> [!NOTE]\n> First line.\n> Second line.',
    contains: ['[!NOTE]', 'First line.', 'Second line.'],
  },
  {
    id: 'callout-nested-marks',
    md: '> [!TIP]\n> Has **bold**, a [link](https://example.com) and `code`.',
    contains: [
      '[!TIP]',
      '**bold**',
      '[link](https://example.com)',
      '`code`',
    ],
  },
  {
    id: 'collapsible-closed',
    md: '<details><summary>Click me</summary>\n\nHidden body text.\n\n</details>',
    contains: ['<details', '</details>', 'Click me', 'Hidden body text.'],
    // edit mode force-opens <details>; the SAVED state was closed, so the
    // serialized form must NOT acquire an `open` attribute.
    notContains: ['<details open', '<details  open'],
  },
  {
    id: 'collapsible-open',
    md: '<details open><summary>Open one</summary>\n\nVisible body.\n\n</details>',
    // the saved-open state must survive the round-trip.
    contains: ['<details', 'open', 'Open one', 'Visible body.'],
  },
  {
    id: 'pull-quote',
    md: ':::quote{cite="Ada Lovelace"}\nThe Analytical Engine.\n:::',
    contains: ['quote', 'Ada Lovelace', 'The Analytical Engine.'],
  },
  {
    id: 'pull-quote-nocite',
    md: ':::quote\nNo attribution here.\n:::',
    contains: ['quote', 'No attribution here.'],
  },
  {
    id: 'tabs',
    md: ':::tabs\n### First\nAlpha content.\n\n### Second\nBeta content.\n:::',
    contains: ['First', 'Second', 'Alpha content.', 'Beta content.'],
  },
  {
    id: 'task-list',
    md: '- [ ] unchecked item\n- [x] checked item',
    contains: ['[ ]', '[x]', 'unchecked item', 'checked item'],
  },
  {
    id: 'table',
    md: '| Name | Role |\n| --- | --- |\n| Ada | Pioneer |\n| Alan | Theorist |',
    contains: ['Name', 'Role', 'Ada', 'Pioneer', 'Alan', 'Theorist'],
  },
  {
    id: 'nested-list',
    md: '- top\n  - middle\n    - leaf',
    contains: ['top', 'middle', 'leaf'],
  },
  {
    id: 'marks',
    md: 'Some **bold** and *italic* and `code` and ==highlighted== words.',
    contains: ['**bold**', '*italic*', '`code`', '==highlighted=='],
  },
  {
    id: 'headings',
    md: '# Title\n\n## Section\n\n### Subsection\n\nBody paragraph.',
    contains: ['# Title', '## Section', '### Subsection', 'Body paragraph.'],
  },
  {
    id: 'code-block',
    md: '```js\nconst x = 1\nconsole.log(x)\n```',
    contains: ['```js', 'const x = 1', 'console.log(x)'],
  },
  {
    id: 'math',
    md: '$$\nE = mc^2\n$$',
    contains: ['$$', 'E = mc^2'],
  },
  {
    id: 'wikilink',
    md: 'See [[Some Page]] for details.',
    contains: ['[[Some Page]]', 'for details.'],
  },
]

// The newest / richest blocks — directive-based data blocks + fenced widgets.
const RICH_CASES: RoundTripCase[] = [
  {
    id: 'kanban',
    md: ':::kanban\n### To do\n- [ ] A card\n\n### In progress\n- [ ] Another card\n:::',
    contains: ['kanban', 'To do', 'In progress', 'A card', 'Another card'],
  },
  {
    id: 'stat-grid',
    md: ':::stats\n### Solar PV LCOE\n**$43** /MWh\n\n↓ 90% since 2010\n:::',
    contains: ['stats', 'Solar PV LCOE', '$43', '/MWh', '90% since 2010'],
  },
  {
    id: 'timeline',
    md: ':::timeline\n- **2026-01-15** v1.0 shipped — first stable release\n- **2026-03-01** v1.1 — search + exports\n:::',
    contains: ['timeline', '2026-01-15', 'v1.0 shipped', '2026-03-01'],
  },
  {
    id: 'calendar',
    md: ':::calendar{month=2026-05}\n- 2026-05-04 Spec freeze\n- 2026-05-28 GA launch\n:::',
    contains: ['calendar', '2026-05', 'Spec freeze', 'GA launch'],
  },
  {
    id: 'embed',
    md: ':::embed\nhttps://www.youtube.com/watch?v=ID\n:::',
    contains: ['embed', 'youtube.com/watch?v=ID'],
  },
  {
    id: 'mermaid',
    md: '```mermaid\ngraph TD\n  A[Start] --> B[End]\n```',
    contains: ['```mermaid', 'graph TD', 'A[Start]', 'B[End]'],
  },
  {
    id: 'chart',
    md: '```chart\ntype: bar\ntitle: Quarterly revenue\nx: [Q1, Q2, Q3, Q4]\n```',
    contains: ['```chart', 'type: bar', 'Quarterly revenue', 'Q1, Q2, Q3, Q4'],
  },
]

// Every case mounts behind a plain anchor paragraph so the round-trip driver
// always has a real text caret to type into — clicking a node-view widget (math
// atom, pull-quote figcaption) doesn't place one.
const ANCHOR = 'Zanchor'
const SENTINEL = 'JJQ'

function RoundTripCaseView({ id, md }: { id: string; md: string }) {
  // out starts empty (NOT md) so play() can wait for the first real
  // serialization to land.
  const [out, setOut] = useState('')
  return (
    <div data-testid={`case-${id}`} style={{ marginBottom: 24 }}>
      <MilkdownEditor
        defaultValue={`${ANCHOR}\n\n${md}`}
        onChange={setOut}
        collabPageId={null}
        ariaLabel={`editor-${id}`}
      />
      <pre data-testid={`out-${id}`} style={{ display: 'none' }}>
        {out}
      </pre>
    </div>
  )
}

function RoundTripHarness({ cases }: { cases: RoundTripCase[] }) {
  const qc = useMemo(
    () => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    [],
  )
  return (
    <QueryClientProvider client={qc}>
      <div style={{ padding: 16, maxWidth: '48rem' }}>
        {cases.map((c) => (
          <RoundTripCaseView key={c.id} id={c.id} md={c.md} />
        ))}
      </div>
    </QueryClientProvider>
  )
}

// Force one editor to re-serialize without permanently changing it: type a
// sentinel into the anchor paragraph, wait for it in the serialized output,
// delete it, and return the now-restored markdown.
async function roundTrip(
  canvasElement: HTMLElement,
  id: string,
): Promise<string> {
  const canvas = within(canvasElement)
  const root = canvasElement.querySelector<HTMLElement>(
    `[data-testid="case-${id}"]`,
  )
  if (!root) throw new Error(`case container missing`)
  const pm = root.querySelector<HTMLElement>('.ProseMirror[contenteditable]')
  if (!pm) throw new Error(`editor never mounted`)
  const anchor = pm.querySelector<HTMLElement>('p')
  if (!anchor) throw new Error(`no anchor paragraph`)

  await userEvent.click(anchor)
  await userEvent.keyboard(SENTINEL)
  await waitFor(
    () => {
      expect(canvas.getByTestId(`out-${id}`).textContent ?? '').toContain(
        SENTINEL,
      )
    },
    { timeout: 6000 },
  )
  for (let i = 0; i < SENTINEL.length; i++) {
    await userEvent.keyboard('{Backspace}')
  }
  let out = ''
  await waitFor(
    () => {
      out = canvas.getByTestId(`out-${id}`).textContent ?? ''
      expect(out).not.toContain(SENTINEL)
    },
    { timeout: 6000 },
  )
  return out
}

function makeRoundTripStory(cases: RoundTripCase[]): Story {
  return {
    render: () => <RoundTripHarness cases={cases} />,
    play: async ({ canvasElement }) => {
      await waitFor(
        () => {
          const n = canvasElement.querySelectorAll(
            '.ProseMirror[contenteditable]',
          ).length
          expect(n).toBe(cases.length)
        },
        { timeout: 15000 },
      )

      const problems: string[] = []
      for (const c of cases) {
        let out = ''
        try {
          out = await roundTrip(canvasElement, c.id)
        } catch (e) {
          problems.push(`[${c.id}] threw: ${(e as Error).message}`)
          continue
        }
        for (const needle of c.contains) {
          if (!out.includes(needle)) {
            problems.push(
              `[${c.id}] DROPPED ${JSON.stringify(needle)} — got: ${JSON.stringify(out)}`,
            )
          }
        }
        for (const bad of c.notContains ?? []) {
          if (out.includes(bad)) {
            problems.push(
              `[${c.id}] LEAKED ${JSON.stringify(bad)} — got: ${JSON.stringify(out)}`,
            )
          }
        }
      }
      expect(problems, `\n${problems.join('\n')}\n`).toEqual([])
    },
  }
}

export const RoundTripsCore = makeRoundTripStory(CORE_CASES)
export const RoundTripsRich = makeRoundTripStory(RICH_CASES)

// ── Scenario 5: a real multi-step editing flow (solo mode) ──────────────────
// One editor, a representative sequence a user actually performs: heading via
// markdown shortcut → paragraph → bold a selection via the bubble toolbar →
// bullet list with a Tab-nested item → slash-insert a callout with a body. We
// assert both the live DOM and the final serialized markdown.
//
// NOTE: this runs in SOLO mode only. The collab path is read-only until the
// provider reaches 'connected' (a server sync-init), and the provider is
// constructed internally against a real /ws URL — so it can't be driven offline
// today. Running this same flow against the collab path is the acceptance test
// for A1 (extracting an injectable useCollabSession).
export const EditingFlow: Story = {
  args: { defaultValue: '' },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement)
    const pm = await getEditable(canvasElement)
    const out = () => canvas.getByTestId('md-out').textContent ?? ''

    // 1. heading via markdown shortcut — and prove onChange serializes it.
    await userEvent.click(pm)
    await userEvent.keyboard('# Project plan')
    await waitFor(() => {
      expect(pm.querySelector('h1')?.textContent).toContain('Project plan')
    })
    await waitFor(() => expect(out()).toContain('# Project plan'), {
      timeout: 6000,
    })

    // 2. new paragraph, bold a selection via the bubble toolbar.
    await userEvent.keyboard('{Enter}')
    await userEvent.keyboard('Status is green')
    const para = Array.from(pm.querySelectorAll('p')).find((p) =>
      p.textContent?.includes('Status is green'),
    )
    expect(para, 'status paragraph should exist').toBeTruthy()
    await userEvent.tripleClick(para as HTMLElement)
    await waitFor(() => {
      expect(document.querySelector('.tela-bubble-toolbar')).not.toBeNull()
    })
    await userEvent.click(
      document.querySelector(
        '.tela-bubble-toolbar [aria-label="Bold"]',
      ) as HTMLElement,
    )
    await waitFor(() => {
      expect(pm.querySelector('strong')?.textContent).toContain(
        'Status is green',
      )
    })

    // 3. a bullet list with a Tab-nested item.
    await userEvent.click(pm.querySelector('strong') as HTMLElement)
    await userEvent.keyboard('{Enter}')
    await userEvent.keyboard('- First item{Enter}Second item{Enter}{Tab}Nested item')
    await waitFor(() => {
      expect(pm.querySelector('ul ul')).not.toBeNull() // nesting happened
    })

    // 4. exit the list and slash-insert a callout with a body.
    await userEvent.keyboard('{Enter}{Enter}')
    await userEvent.keyboard('/callout')
    await waitFor(() => {
      expect(document.querySelector('.tela-slash-menu')).not.toBeNull()
    })
    await userEvent.keyboard('{Enter}')
    await waitFor(() => {
      expect(pm.querySelector('.tela-callout')).not.toBeNull()
    })
    await userEvent.keyboard('Heads up note')

    // live DOM has every element we built.
    expect(pm.querySelector('h1')).not.toBeNull()
    expect(pm.querySelector('strong')).not.toBeNull()
    expect(pm.querySelector('ul ul')).not.toBeNull()
    expect(pm.querySelector('.tela-callout')?.textContent).toContain(
      'Heads up note',
    )

    // final serialized markdown carries the whole flow.
    await waitFor(
      () => {
        const md = out()
        expect(md).toContain('# Project plan')
        expect(md).toContain('**Status is green**')
        expect(md).toContain('First item')
        expect(md).toContain('Nested item')
        expect(md).toContain('[!NOTE]')
        expect(md).toContain('Heads up note')
      },
      { timeout: 6000 },
    )
  },
}

// ── Scenario 4: live markdown input rules (the editing-feel net) ─────────────
// Typing a markdown prefix should transform the block AS YOU TYPE, the way every
// rich block (callouts, math, highlight) already does. This pins which CommonMark
// shortcuts actually fire live — heading is the one we suspected was missing.
const SHORTCUTS: { id: string; type: string; sel: string; text: string }[] = [
  { id: 'h1', type: '# ', sel: 'h1', text: 'Heading one' },
  { id: 'h2', type: '## ', sel: 'h2', text: 'Heading two' },
  { id: 'bullet', type: '- ', sel: 'ul li', text: 'a bullet' },
  { id: 'ordered', type: '1. ', sel: 'ol li', text: 'first' },
  { id: 'quote', type: '> ', sel: 'blockquote', text: 'a quote' },
]

// Blank editors (no anchor) so the caret starts at the head of an empty
// paragraph — CommonMark input rules only fire at the START of a textblock.
function BlankHarness({ ids }: { ids: string[] }) {
  const qc = useMemo(
    () => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    [],
  )
  return (
    <QueryClientProvider client={qc}>
      <div style={{ padding: 16, maxWidth: '48rem' }}>
        {ids.map((id) => (
          <div key={id} data-testid={`case-${id}`} style={{ marginBottom: 24 }}>
            <MilkdownEditor
              defaultValue=""
              onChange={() => {}}
              collabPageId={null}
              ariaLabel={`editor-${id}`}
            />
          </div>
        ))}
      </div>
    </QueryClientProvider>
  )
}

export const LiveMarkdownShortcuts: Story = {
  render: () => <BlankHarness ids={SHORTCUTS.map((s) => s.id)} />,
  play: async ({ canvasElement }) => {
    await waitFor(
      () => {
        expect(
          canvasElement.querySelectorAll('.ProseMirror[contenteditable]')
            .length,
        ).toBe(SHORTCUTS.length)
      },
      { timeout: 15000 },
    )

    const problems: string[] = []
    for (const s of SHORTCUTS) {
      const root = canvasElement.querySelector<HTMLElement>(
        `[data-testid="case-${s.id}"]`,
      )
      const pm = root?.querySelector<HTMLElement>(
        '.ProseMirror[contenteditable]',
      )
      if (!pm) {
        problems.push(`[${s.id}] editor missing`)
        continue
      }
      // empty editor → caret at the start of its lone empty paragraph.
      await userEvent.click(pm)
      await userEvent.keyboard(`${s.type}${s.text}`)
      try {
        await waitFor(
          () => {
            const el = pm.querySelector(s.sel)
            expect(el, `typing ${JSON.stringify(s.type)} did not create <${s.sel}>`).not.toBeNull()
            expect(el?.textContent ?? '').toContain(s.text)
          },
          { timeout: 4000 },
        )
      } catch (e) {
        problems.push(`[${s.id}] ${(e as Error).message}`)
      }
    }
    expect(problems, `\n${problems.join('\n')}\n`).toEqual([])
  },
}
