import type { Meta, StoryObj } from '@storybook/react-vite'
import { DeckCoverImage } from './deck-cover-image'

// A 16:9 SVG data-URI so stories don't depend on the render sidecar.
const sample =
  'data:image/svg+xml;utf8,' +
  encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" width="640" height="360"><rect width="640" height="360" fill="#1e293b"/><text x="50%" y="50%" fill="#e2e8f0" font-family="sans-serif" font-size="40" text-anchor="middle" dominant-baseline="middle">Slide 1</text></svg>`,
  )

const meta: Meta<typeof DeckCoverImage> = {
  title: 'App/DeckCoverImage',
  component: DeckCoverImage,
  decorators: [
    (Story) => (
      <div className="relative aspect-video w-[28rem] overflow-hidden rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-2)]">
        <Story />
      </div>
    ),
  ],
}
export default meta

type Story = StoryObj<typeof DeckCoverImage>

export const Loaded: Story = {
  args: { src: sample, className: 'size-full object-cover' },
}

// Points at a URL that will never load — exercises the skeleton + retry path
// (it stays on the pulsing skeleton through the backoff retries).
export const FailingRetries: Story = {
  args: { src: 'https://invalid.example/never.png', className: 'size-full object-cover' },
}
