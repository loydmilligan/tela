import type { Meta, StoryObj } from '@storybook/react-vite'
import type { ReactNode } from 'react'

// Showcase the GFM task-list chrome. The editor renders this exact DOM via the
// gfm preset's task-list-item toDOM (`li[data-item-type="task"]
// [data-checked]`) inside `.tela-milkdown .ProseMirror`; here we hand-render
// the same structure so the story exercises the editor.css checkbox styling
// without a Milkdown mount. Clicking does nothing in the story (the toggle is
// the taskCheckboxPlugin, which needs a live EditorView) — this is a visual
// reference for the checked / unchecked / mixed states.

interface TaskListPreviewProps {
  items: { checked: boolean; body: ReactNode }[]
}

function TaskListPreview({ items }: TaskListPreviewProps) {
  return (
    <div className="tela-milkdown">
      <div className="ProseMirror">
        <ul>
          {items.map((item, i) => (
            <li
              key={i}
              data-item-type="task"
              data-checked={item.checked ? 'true' : 'false'}
            >
              <p>{item.body}</p>
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}

const meta: Meta<typeof TaskListPreview> = {
  title: 'App/Milkdown Task List',
  component: TaskListPreview,
  parameters: { layout: 'padded' },
}
export default meta

type Story = StoryObj<typeof TaskListPreview>

export const Unchecked: Story = {
  args: { items: [{ checked: false, body: 'Draft the release notes' }] },
}

export const Checked: Story = {
  args: { items: [{ checked: true, body: 'Cut the 0.4.0 tag' }] },
}

export const Mixed: Story = {
  name: 'Checklist — mixed state',
  render: () => (
    <div className="max-w-[40rem]">
      <TaskListPreview
        items={[
          { checked: true, body: 'Wire the GFM task-list schema' },
          { checked: true, body: 'Draw the checkbox in editor.css' },
          { checked: false, body: 'Add click-to-toggle' },
          { checked: false, body: 'Add the slash-menu inserter' },
        ]}
      />
    </div>
  ),
}
