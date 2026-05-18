import type { Meta, StoryObj } from '@storybook/react-vite'
import { SaveIndicator } from './save-indicator'

const meta: Meta<typeof SaveIndicator> = {
  title: 'UI/SaveIndicator',
  component: SaveIndicator,
  argTypes: {
    status: {
      control: 'select',
      options: ['idle', 'saving', 'saved', 'retrying', 'error'],
    },
  },
  args: { status: 'saving' },
}
export default meta

type Story = StoryObj<typeof SaveIndicator>

export const Saving: Story = { args: { status: 'saving' } }
export const Saved: Story = { args: { status: 'saved' } }
export const Retrying: Story = { args: { status: 'retrying' } }
export const Error: Story = { args: { status: 'error' } }

export const States: Story = {
  render: () => (
    <div className="flex flex-col gap-[var(--space-3)]">
      <SaveIndicator status="saving" />
      <SaveIndicator status="saved" />
      <SaveIndicator status="retrying" />
      <SaveIndicator status="error" />
    </div>
  ),
}
