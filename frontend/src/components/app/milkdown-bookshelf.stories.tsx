import { useMemo, useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { expect, waitFor } from 'storybook/test'
import { MilkdownEditor } from './milkdown-editor'
import { MarkdownView } from '../view/MarkdownView'
import { primeBookmarkMeta } from '../../lib/blocks/bookmark'

// Bookmark cards + bookshelf: the editor nodeViews AND the read-only view
// renderer, fed identical primed unfurl meta (no network in stories).

const meta = {
  title: 'App/Bookshelf',
  parameters: { layout: 'fullscreen' },
} satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

const LINKS = [
  {
    url: 'https://example.com/guide',
    title: 'The Example Guide to Everything',
    description: 'A long-form reference with all the examples you could want.',
    site_name: 'Example',
    favicon: '',
    image: '',
  },
  {
    url: 'https://example.org/paper?id=42&ref=x',
    title: 'A Paper With Query Params',
    description: 'Ampersands in the URL must survive the round-trip.',
    site_name: 'Example Org',
    favicon: '',
    image: '',
  },
]
LINKS.forEach(primeBookmarkMeta)

const MD = `A single bookmark via embed:

:::embed
${LINKS[0].url}
:::

:::bookshelf{style="expanded"}
* <${LINKS[0].url}>
* <${LINKS[1].url}>
:::

:::bookshelf{style="compact"}
* <${LINKS[0].url}>
* <${LINKS[1].url}>
:::
`

function EditorHarness() {
  const qc = useMemo(
    () => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    [],
  )
  const [out, setOut] = useState('')
  return (
    <QueryClientProvider client={qc}>
      <div style={{ padding: 16, maxWidth: '48rem' }}>
        <MilkdownEditor
          defaultValue={MD}
          onChange={setOut}
          collabPageId={null}
          ariaLabel="bookshelf editor"
        />
        <pre data-testid="md-out" style={{ display: 'none' }}>
          {out}
        </pre>
      </div>
    </QueryClientProvider>
  )
}

export const EditorCards: Story = {
  render: () => <EditorHarness />,
  play: async ({ canvasElement }) => {
    await waitFor(
      () => {
        // Both bookshelves render their cards; the embed renders one card.
        const cards = canvasElement.querySelectorAll('.tela-bookmark')
        expect(cards.length).toBe(5)
        // Primed meta fills titles without any network.
        expect(canvasElement.textContent).toContain('The Example Guide to Everything')
        expect(canvasElement.textContent).toContain('A Paper With Query Params')
        // Compact shelf renders rows.
        expect(canvasElement.querySelectorAll('.tela-bookmark-row').length).toBe(2)
      },
      { timeout: 15000 },
    )
  },
}

function ViewHarness() {
  const qc = useMemo(
    () => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
    [],
  )
  return (
    <QueryClientProvider client={qc}>
      <div className="tela-reader" style={{ padding: 16, maxWidth: '48rem' }}>
        <MarkdownView body={MD} />
      </div>
    </QueryClientProvider>
  )
}

export const ViewCards: Story = {
  render: () => <ViewHarness />,
  play: async ({ canvasElement }) => {
    await waitFor(
      () => {
        expect(canvasElement.querySelectorAll('.tela-bookmark').length).toBe(5)
        expect(canvasElement.textContent).toContain('The Example Guide to Everything')
        expect(
          canvasElement.querySelectorAll("[data-style='compact'] .tela-bookmark-row").length,
        ).toBe(2)
      },
      { timeout: 15000 },
    )
  },
}
