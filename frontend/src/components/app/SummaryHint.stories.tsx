import type { Meta, StoryObj } from '@storybook/react-vite'
import { SummaryHint } from './SummaryHint'

const meta = {
  title: 'App/SummaryHint',
  component: SummaryHint,
  parameters: { layout: 'padded' },
  // The hint lives in a title's left gutter and is revealed by hovering the
  // `group` wrapper — stories reproduce that placement.
  decorators: [
    (Story) => (
      <div className="group relative max-w-[36rem] pl-[var(--space-7)]">
        <Story />
        <h1 className="m-0 text-[length:var(--text-3xl)] leading-[var(--leading-tight)] font-medium text-[var(--text-primary)]">
          The Token Tax
        </h1>
      </div>
    ),
  ],
} satisfies Meta<typeof SummaryHint>

export default meta
type Story = StoryObj<typeof meta>

export const Default: Story = {
  args: {
    summary:
      'What actually moves token cost, measured end-to-end against Claude Opus 4.8. Language is the biggest lever; politeness is free.',
    className: 'absolute left-0 top-[var(--space-1)]',
  },
}

export const LongSummary: Story = {
  args: {
    summary:
      'A comparative report on solar and wind power covering cost trends, capacity growth, notable milestones, and an honest accounting of where each technology wins or loses depending on geography, storage availability, and grid maturity.',
    className: 'absolute left-0 top-[var(--space-1)]',
  },
}
