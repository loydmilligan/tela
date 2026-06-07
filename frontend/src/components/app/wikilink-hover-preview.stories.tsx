import type { Meta, StoryObj } from '@storybook/react-vite'
import { WikilinkPreviewCard } from './wikilink-hover-preview'

// The card is position:fixed and anchors to a link rect. In Storybook we feed a
// fixed rect near the top-left so it renders predictably in the canvas.
const rect = { left: 40, top: 80, bottom: 100 }

const meta: Meta<typeof WikilinkPreviewCard> = {
  title: 'App/WikilinkPreviewCard',
  component: WikilinkPreviewCard,
  parameters: { layout: 'fullscreen' },
}
export default meta

type Story = StoryObj<typeof WikilinkPreviewCard>

export const Loaded: Story = {
  args: {
    rect,
    title: 'Tela as Agent Backend & Sync',
    excerpt:
      'Use tela as the backing store for agent knowledge / a team vault, where agents and humans work through native local files and/or MCP against one Postgres-canonical store.',
    loading: false,
  },
}

export const Loading: Story = {
  args: { rect, title: 'Implementation Spec — Sync Feature', excerpt: '', loading: true },
}

export const Empty: Story = {
  args: { rect, title: 'Scratchpad', excerpt: '', loading: false },
}

export const LongTitleClamps: Story = {
  args: {
    rect,
    title:
      'A deliberately very long page title that should clamp to two lines instead of overflowing the preview card boundary',
    excerpt:
      'Body excerpt clamps to four lines. '.repeat(20),
    loading: false,
  },
}
