import type { Meta, StoryObj } from '@storybook/react-vite'
import { Sparkline } from './sparkline'

const meta: Meta<typeof Sparkline> = {
  title: 'UI/Sparkline',
  component: Sparkline,
  args: {
    values: [3, 5, 4, 8, 6, 9, 7, 12, 10, 14, 11, 16],
    width: 160,
    height: 40,
    area: true,
  },
}
export default meta

type Story = StoryObj<typeof Sparkline>

export const Accent: Story = {
  render: (args) => (
    <span className="inline-block text-[var(--accent)]">
      <Sparkline {...args} />
    </span>
  ),
}

export const LineOnly: Story = {
  render: (args) => (
    <span className="inline-block text-[var(--text-primary)]">
      <Sparkline {...args} area={false} />
    </span>
  ),
}

export const Flat: Story = {
  render: (args) => (
    <span className="inline-block text-[var(--text-muted)]">
      <Sparkline {...args} values={[4, 4, 4, 4, 4]} />
    </span>
  ),
}
