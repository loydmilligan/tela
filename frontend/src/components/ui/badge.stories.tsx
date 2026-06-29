import type { Meta, StoryObj } from '@storybook/react-vite'
import { Badge } from './badge'

const meta: Meta<typeof Badge> = {
  title: 'UI/Badge',
  component: Badge,
  argTypes: {
    variant: { control: 'select', options: ['muted', 'accent', 'danger'] },
  },
  args: { children: 'This device' },
}
export default meta

type Story = StoryObj<typeof Badge>

export const Muted: Story = { args: { variant: 'muted' } }
export const Accent: Story = { args: { variant: 'accent' } }
export const Danger: Story = { args: { variant: 'danger', children: 'Bug' } }

export const Variants: Story = {
  render: () => (
    <div className="flex flex-wrap gap-[var(--space-3)] items-center">
      <Badge variant="muted">Engineering</Badge>
      <Badge variant="accent">This device</Badge>
      <Badge variant="danger">Bug</Badge>
    </div>
  ),
}
