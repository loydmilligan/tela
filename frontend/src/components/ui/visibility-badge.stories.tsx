import type { Meta, StoryObj } from '@storybook/react-vite'
import { VisibilityBadge } from './visibility-badge'

const meta: Meta<typeof VisibilityBadge> = {
  title: 'UI/VisibilityBadge',
  component: VisibilityBadge,
  argTypes: {
    state: { control: 'select', options: ['private', 'public', 'password'] },
    inherited: { control: 'boolean' },
    compact: { control: 'boolean' },
  },
  args: { state: 'public', inherited: false, compact: false },
}
export default meta

type Story = StoryObj<typeof VisibilityBadge>

export const Public: Story = { args: { state: 'public' } }
export const Password: Story = { args: { state: 'password' } }
export const SpaceOnly: Story = { args: { state: 'private' } }
export const Inherited: Story = { args: { state: 'public', inherited: true } }

export const AllStates: Story = {
  render: () => (
    <div className="flex flex-wrap gap-[var(--space-3)] items-center">
      <VisibilityBadge state="private" />
      <VisibilityBadge state="public" />
      <VisibilityBadge state="password" />
      <VisibilityBadge state="public" inherited />
      <VisibilityBadge state="password" inherited />
    </div>
  ),
}

export const Compact: Story = {
  render: () => (
    <div className="flex flex-wrap gap-[var(--space-2)] items-center">
      <VisibilityBadge state="private" compact />
      <VisibilityBadge state="public" compact />
      <VisibilityBadge state="password" compact />
      <VisibilityBadge state="public" inherited compact />
    </div>
  ),
}
