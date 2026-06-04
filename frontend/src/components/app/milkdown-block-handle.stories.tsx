import type { Meta, StoryObj } from '@storybook/react-vite'
import {
  Code,
  Copy,
  GripVertical,
  Heading1,
  Heading2,
  Heading3,
  List,
  ListOrdered,
  Plus,
  Quote,
  Trash2,
  Type,
} from 'lucide-react'

// Visual reference for the block-handle gutter + action menu. The live
// component (BlockHandleView) needs a Milkdown editor + BlockProvider for
// hover-tracking and drag, so — like the other editor-plugin stories — this
// renders the same DOM/classes statically. Both elements are `position: fixed`
// and hidden until shown; the preview forces them static + visible.

const meta: Meta = {
  title: 'App/Milkdown Block Handle',
  parameters: { layout: 'padded' },
}
export default meta

type Story = StoryObj

export const Gutter: Story = {
  name: 'Gutter (+ / drag handle)',
  render: () => (
    <div
      className="tela-block-handle"
      data-show="true"
      style={{ position: 'static' }}
    >
      <button
        type="button"
        className="tela-block-handle-btn"
        aria-label="Add block below"
      >
        <Plus size="1em" strokeWidth={2.5} aria-hidden />
      </button>
      <button
        type="button"
        className="tela-block-handle-btn tela-block-handle-grip"
        aria-label="Drag to move; click for actions"
      >
        <GripVertical size="1em" strokeWidth={2.5} aria-hidden />
      </button>
    </div>
  ),
}

const TURN_INTO = [
  { label: 'Text', Icon: Type },
  { label: 'Heading 1', Icon: Heading1 },
  { label: 'Heading 2', Icon: Heading2 },
  { label: 'Heading 3', Icon: Heading3 },
  { label: 'Bulleted list', Icon: List },
  { label: 'Numbered list', Icon: ListOrdered },
  { label: 'Quote', Icon: Quote },
  { label: 'Code block', Icon: Code },
]

export const ActionMenu: Story = {
  name: 'Block-action menu (with Turn into)',
  render: () => (
    <div className="tela-block-menu" role="menu" style={{ position: 'static' }}>
      <div className="tela-block-menu-label">Turn into</div>
      {TURN_INTO.map(({ label, Icon }) => (
        <button
          key={label}
          type="button"
          role="menuitem"
          className="tela-block-menu-item"
        >
          <Icon size="1em" aria-hidden />
          <span>{label}</span>
        </button>
      ))}
      <div className="tela-block-menu-sep" role="separator" />
      <button type="button" role="menuitem" className="tela-block-menu-item">
        <Copy size="1em" aria-hidden />
        <span>Duplicate</span>
      </button>
      <button type="button" role="menuitem" className="tela-block-menu-item">
        <Trash2 size="1em" aria-hidden />
        <span>Delete</span>
      </button>
    </div>
  ),
}
