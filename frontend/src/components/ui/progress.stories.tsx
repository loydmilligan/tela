import type { Meta, StoryObj } from '@storybook/react-vite'
import { Progress } from './progress'

const meta: Meta<typeof Progress> = {
  title: 'UI/Progress',
  component: Progress,
  argTypes: {
    value: { control: 'range', min: 0, max: 100 },
    max: { control: 'number' },
    tone: {
      control: 'select',
      options: ['auto', 'neutral', 'warning', 'danger'],
    },
  },
  args: { value: 60, max: 100, tone: 'auto' },
  decorators: [
    (Story) => (
      <div style={{ width: 320 }}>
        <Story />
      </div>
    ),
  ],
}
export default meta

type Story = StoryObj<typeof Progress>

export const Default: Story = {}

// Auto-tone escalation across fill levels.
export const ToneEscalation: Story = {
  render: () => (
    <div className="flex flex-col gap-[var(--space-4)]">
      <Progress value={30} max={100} />
      <Progress value={85} max={100} />
      <Progress value={100} max={100} />
    </div>
  ),
}

// Unlimited plan: a calm, dimmed full track (no cap to fill against).
export const Unlimited: Story = { args: { value: 9999, max: null } }
