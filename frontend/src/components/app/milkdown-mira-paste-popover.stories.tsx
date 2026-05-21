import type { Meta, StoryObj } from '@storybook/react-vite'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { MiraPastePopover } from './milkdown-mira-paste-popover'

// Showcase the M18.B.2 paste-hook popover surface. Click handlers are mocked
// (logged) — the popover's import path is gated through `useImportMira` /
// `useMutation`, so the story wraps it in a QueryClientProvider with retries
// disabled and renders against a fixed anchor at the top-left so the
// position-fixed chrome is visible inside the Storybook canvas.

function withQueryClient(Story: () => ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return (
    <QueryClientProvider client={client}>
      <div style={{ position: 'relative', minHeight: '20rem' }}>{Story()}</div>
    </QueryClientProvider>
  )
}

const meta: Meta<typeof MiraPastePopover> = {
  title: 'App/MiraPastePopover',
  component: MiraPastePopover,
  decorators: [withQueryClient],
  parameters: {
    layout: 'fullscreen',
  },
}
export default meta

type Story = StoryObj<typeof MiraPastePopover>

const noop = () => {}

export const Default: Story = {
  args: {
    url: 'https://mira.cagdas.io/p/showcase',
    anchor: { left: 64, top: 64, bottom: 80 },
    spaceId: 1,
    parentPageId: 1,
    onImportComplete: noop,
    onKeepAsLink: noop,
    onCancel: noop,
  },
}

export const LongUrl: Story = {
  args: {
    url: 'https://mira.cagdas.io/p/very-long-mira-page-slug-that-should-truncate-with-ellipsis-when-the-popover-clamps-its-width-around-the-action-row',
    anchor: { left: 64, top: 64, bottom: 80 },
    spaceId: 1,
    parentPageId: 1,
    onImportComplete: noop,
    onKeepAsLink: noop,
    onCancel: noop,
  },
}
