import { useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import { expect, userEvent, within } from 'storybook/test'
import { CommentComposer } from './CommentComposer'

const meta: Meta<typeof CommentComposer> = {
  title: 'App/CommentComposer',
  component: CommentComposer,
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

type Story = StoryObj<typeof CommentComposer>

const anchor = { prefix: 'the ', exact: 'quick brown fox', suffix: ' jumps' }

// Captures the last submitted payload so a play test can assert exactly what the
// composer sends to createCommentCore.
function Harness({
  onCapture,
}: {
  onCapture?: (input: Record<string, unknown>) => void
}) {
  const [last, setLast] = useState<Record<string, unknown> | null>(null)
  return (
    <div className="flex flex-col gap-[var(--space-3)]">
      <CommentComposer
        hasSelection
        anchorPreview={anchor.exact}
        captureAnchor={() => anchor}
        onSubmit={async (input) => {
          setLast(input as Record<string, unknown>)
          onCapture?.(input as Record<string, unknown>)
        }}
      />
      <pre data-testid="submitted" className="text-[length:var(--text-xs)]">
        {last ? JSON.stringify(last) : ''}
      </pre>
    </div>
  )
}

export const Default: Story = {
  render: () => <Harness />,
}

// A plain comment carries no props — change mode is off by default, so the
// common case is unchanged by this feature.
export const PlainCommentSendsNoProps: Story = {
  render: () => <Harness />,
  play: async ({ canvasElement }) => {
    const c = within(canvasElement)
    await userEvent.type(c.getByLabelText('New comment body'), 'just a remark')
    await userEvent.click(c.getByRole('button', { name: 'Comment' }))
    const out = JSON.parse(c.getByTestId('submitted').textContent || '{}')
    await expect(out.body).toBe('just a remark')
    await expect(out.props).toBeUndefined()
  },
}

// Change mode attaches the structured bag that makes the comment queryable via
// `query{ target: comments }` — under change_summary, NOT summary (that key is
// the page's own abstract).
export const ChangeCommentAttachesProps: Story = {
  render: () => <Harness />,
  play: async ({ canvasElement }) => {
    const c = within(canvasElement)
    await userEvent.type(c.getByLabelText('New comment body'), 'bumped the index')
    await userEvent.click(c.getByLabelText('Log this comment as a change'))
    await userEvent.type(
      c.getByLabelText('Change summary'),
      'switched to jsonb_path_ops',
    )
    await userEvent.selectOptions(c.getByLabelText('Change status'), 'done')
    await userEvent.click(c.getByRole('button', { name: 'Log change' }))

    const out = JSON.parse(c.getByTestId('submitted').textContent || '{}')
    await expect(out.props).toEqual({
      type: 'change',
      change_summary: 'switched to jsonb_path_ops',
      status: 'done',
    })
    // The key it must never use.
    await expect(out.props.summary).toBeUndefined()
  },
}

// A change with no summary is refused before it reaches the server — the
// changelog is worthless if entries have no headline.
export const ChangeRequiresASummary: Story = {
  render: () => <Harness />,
  play: async ({ canvasElement }) => {
    const c = within(canvasElement)
    await userEvent.type(c.getByLabelText('New comment body'), 'no summary given')
    await userEvent.click(c.getByLabelText('Log this comment as a change'))
    await userEvent.click(c.getByRole('button', { name: 'Log change' }))
    await expect(c.getByRole('alert')).toHaveTextContent(
      'A change comment needs a summary.',
    )
    await expect(c.getByTestId('submitted')).toHaveTextContent('')
  },
}
