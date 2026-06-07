import type { Meta, StoryObj } from '@storybook/react-vite'
import type { ReactNode } from 'react'

// Showcase the pull-quote chrome. The editor renders the same structure via the
// milkdown-pullquote.ts nodeView (figure > blockquote body + figcaption cite);
// here it's static DOM inside a `.tela-milkdown .ProseMirror` wrapper so the
// scoped CSS applies without a Milkdown mount.

interface PullquotePreviewProps {
  cite?: string
  body: ReactNode
}

function PullquotePreview({ cite, body }: PullquotePreviewProps) {
  return (
    <div className="tela-milkdown">
      <div className="ProseMirror">
        <figure className="tela-pullquote" data-cite={cite ?? ''}>
          <blockquote className="tela-pullquote-body">{body}</blockquote>
          {cite ? (
            <figcaption className="tela-pullquote-cite" data-empty="false">
              {cite}
            </figcaption>
          ) : null}
        </figure>
      </div>
    </div>
  )
}

const meta: Meta<typeof PullquotePreview> = {
  title: 'App/Milkdown Pull Quote',
  component: PullquotePreview,
  parameters: { layout: 'padded' },
}
export default meta

type Story = StoryObj<typeof PullquotePreview>

export const WithAttribution: Story = {
  args: {
    cite: 'Steve Jobs',
    body: <p>The only way to do great work is to love what you do.</p>,
  },
}

export const NoAttribution: Story = {
  args: {
    body: (
      <p>
        Markdown is canonical forever — there is no block table, and there never
        will be.
      </p>
    ),
  },
}

export const EditablePlaceholder: Story = {
  name: 'Editable — empty caption placeholder',
  render: () => (
    <div className="tela-milkdown">
      <div className="ProseMirror">
        <figure className="tela-pullquote" data-cite="">
          <blockquote className="tela-pullquote-body">
            <p>A quote still being attributed.</p>
          </blockquote>
          <figcaption
            className="tela-pullquote-cite"
            data-empty="true"
            data-placeholder="Attribution (optional)"
            contentEditable
            suppressContentEditableWarning
          />
        </figure>
      </div>
    </div>
  ),
}
