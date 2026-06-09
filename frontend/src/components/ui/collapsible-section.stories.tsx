import type { Meta, StoryObj } from '@storybook/react-vite'
import { CollapsibleSection } from './collapsible-section'

const meta: Meta<typeof CollapsibleSection> = {
  title: 'UI/CollapsibleSection',
  component: CollapsibleSection,
  args: {
    title: '3 pages link here',
    children: (
      <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)] text-[length:var(--text-sm)] text-[var(--text-primary)]">
        <li>Getting Started</li>
        <li>Using Tela</li>
        <li>Administration</li>
      </ul>
    ),
  },
  parameters: { layout: 'padded' },
}
export default meta

type Story = StoryObj<typeof CollapsibleSection>

export const Collapsed: Story = {}

export const Expanded: Story = { args: { defaultOpen: true } }

export const ConnectionsGraph: Story = {
  args: {
    title: 'Connections',
    mountOnOpen: true,
    children: (
      <div className="h-[calc(var(--space-8)*5)] grid place-items-center rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-1)] text-[var(--text-muted)] text-[length:var(--text-sm)]">
        graph mounts here only after first open
      </div>
    ),
  },
}
