import { useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import { FieldWidget } from './FieldWidget'
import type { FieldSpec } from '../../lib/blocks/field-widget'

const meta: Meta<typeof FieldWidget> = {
  title: 'App/FieldWidget',
  component: FieldWidget,
  parameters: { layout: 'centered' },
  decorators: [
    (Story) => (
      <div style={{ width: 'calc(var(--space-8) * 10)', maxWidth: '92vw' }}>
        <Story />
      </div>
    ),
  ],
}
export default meta

type Story = StoryObj<typeof FieldWidget>

// A live wrapper so the stories are interactive — commit updates local state,
// exactly as FieldBlockView re-reads props after a write.
function Live({ spec, initial }: { spec: FieldSpec; initial: unknown }) {
  const [value, setValue] = useState<unknown>(initial)
  return (
    <FieldWidget
      spec={spec}
      value={value}
      canEdit
      onCommit={setValue}
    />
  )
}

const selectSpec: FieldSpec = {
  prop: 'result',
  type: 'select',
  label: 'Login flow result',
  options: ['pass', 'fail', 'pending'],
}

// The canonical UAT field — a pass/fail/pending select.
export const Select: Story = {
  render: () => <Live spec={selectSpec} initial="pending" />,
}

export const SelectUnset: Story = {
  render: () => <Live spec={selectSpec} initial={undefined} />,
}

export const Toggle: Story = {
  render: () => (
    <Live
      spec={{ prop: 'done', type: 'toggle', label: 'Marked done', options: [] }}
      initial={false}
    />
  ),
}

export const Text: Story = {
  render: () => (
    <Live
      spec={{ prop: 'owner', type: 'text', label: 'Assigned to', options: [] }}
      initial="ada"
    />
  ),
}

export const ButtonField: Story = {
  render: () => (
    <Live
      spec={{
        prop: 'result',
        type: 'button',
        label: 'Mark pass',
        options: [],
        value: 'pass',
      }}
      initial={undefined}
    />
  ),
}

// Read-only surface (public / share, or a viewer): the value shows but can't be
// changed.
export const ReadOnly: Story = {
  args: {
    spec: selectSpec,
    value: 'pass',
    canEdit: false,
    onCommit: () => {},
  },
}

// A write in flight — controls disabled.
export const Pending: Story = {
  args: {
    spec: selectSpec,
    value: 'pass',
    canEdit: true,
    pending: true,
    onCommit: () => {},
  },
}
