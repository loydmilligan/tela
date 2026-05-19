import type { Meta, StoryObj } from '@storybook/react-vite'
import { Avatar } from './avatar'

const meta: Meta<typeof Avatar> = {
  title: 'UI/Avatar',
  component: Avatar,
  argTypes: {
    size: { control: 'select', options: ['sm', 'md', 'lg'] },
    tone: {
      control: 'select',
      options: [
        'neutral',
        'collab-1',
        'collab-2',
        'collab-3',
        'collab-4',
        'collab-5',
        'collab-6',
        'collab-7',
        'collab-8',
      ],
    },
  },
  args: { children: 'A' },
}
export default meta

type Story = StoryObj<typeof Avatar>

export const Default: Story = { args: { tone: 'collab-1' } }

export const Sizes: Story = {
  render: () => (
    <div className="flex items-end gap-[var(--space-3)]">
      <Avatar size="sm" tone="collab-1">
        A
      </Avatar>
      <Avatar size="md" tone="collab-4">
        B
      </Avatar>
      <Avatar size="lg" tone="collab-7">
        C
      </Avatar>
    </div>
  ),
}

export const Tones: Story = {
  render: () => (
    <div className="flex flex-wrap gap-[var(--space-2)]">
      {(
        [
          'neutral',
          'collab-1',
          'collab-2',
          'collab-3',
          'collab-4',
          'collab-5',
          'collab-6',
          'collab-7',
          'collab-8',
        ] as const
      ).map((tone, i) => (
        <Avatar key={tone} tone={tone}>
          {String.fromCharCode(65 + i)}
        </Avatar>
      ))}
    </div>
  ),
}

// Stack — the M7.4 presence avatars layout. Negative margin lets each tile
// overlap its left neighbour; a surface-1 ring keeps the layered edges
// readable across themes.
export const Stack: Story = {
  render: () => (
    <div className="flex items-center">
      {(['collab-1', 'collab-4', 'collab-6', 'collab-8'] as const).map(
        (tone, i) => (
          <Avatar
            key={tone}
            size="sm"
            tone={tone}
            className="-ml-[var(--space-2)] first:ml-0 ring-2 ring-[var(--surface-1)]"
          >
            {String.fromCharCode(65 + i)}
          </Avatar>
        ),
      )}
      <Avatar
        size="sm"
        tone="neutral"
        className="-ml-[var(--space-2)] ring-2 ring-[var(--surface-1)]"
      >
        +3
      </Avatar>
    </div>
  ),
}
