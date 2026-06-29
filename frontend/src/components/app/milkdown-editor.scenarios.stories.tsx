import { useMemo, useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { expect, userEvent, waitFor, within } from 'storybook/test'
import { MilkdownEditor } from './milkdown-editor'

// Behavioural tests that drive the REAL editor (solo / non-collab mode,
// collabPageId=null) end to end: markdown in → ProseMirror render → user edit →
// markdown out. The block insert mechanics are unit-tested in
// lib/milkdown/insert-block.test.ts; these stories are the integration net that
// the unit tests can't give — that the slash menu, schemas, node views and
// serializer actually wire together. This is the first interaction-test harness
// in the repo; reuse the Harness for future editor scenarios.

function Harness({ defaultValue = '' }: { defaultValue?: string }) {
  const qc = useMemo(
    () => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    [],
  )
  // Mirror the latest serialized markdown into the DOM so play() can assert the
  // round-trip without reaching into the editor internals.
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
// A page body with rich markdown renders the matching block chrome. Net for the
// parse → schema → node-view path (and the future edit/read rendering unify).
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
      // callout chrome rendered from `> [!NOTE]`
      expect(pm.querySelector('.tela-callout')).not.toBeNull()
      // heading + list rendered
      expect(pm.querySelector('h1')).not.toBeNull()
      expect(pm.querySelector('ul')).not.toBeNull()
    })
    expect(pm.querySelector('h1')?.textContent).toContain('Heading one')
  },
}

// ── Scenario 2: slash-insert a callout, then type into its body ──────────────
// Drives the slash menu the way a user does and asserts the caret lands inside
// the new callout's body (the insertBlock 'inside' contract) by typing and
// seeing the text appear in the body + serialized markdown.
export const SlashInsertCallout: Story = {
  args: { defaultValue: '' },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement)
    const pm = await getEditable(canvasElement)

    await userEvent.click(pm)
    await userEvent.keyboard('/callout')
    // slash menu appears as a floating list
    await waitFor(() => {
      expect(document.querySelector('.tela-slash-menu')).not.toBeNull()
    })
    await userEvent.keyboard('{Enter}')

    // a callout now exists and the caret is inside its (empty) body — typing
    // lands there.
    await waitFor(() => {
      expect(pm.querySelector('.tela-callout')).not.toBeNull()
    })
    await userEvent.keyboard('Body text')
    await waitFor(() => {
      expect(pm.querySelector('.tela-callout')?.textContent).toContain(
        'Body text',
      )
    })
    // round-trips to canonical callout markdown.
    await waitFor(() => {
      const out = canvas.getByTestId('md-out').textContent ?? ''
      expect(out).toContain('[!NOTE]')
      expect(out).toContain('Body text')
    })
  },
}
