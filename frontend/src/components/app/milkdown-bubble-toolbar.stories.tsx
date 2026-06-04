import type { Meta, StoryObj } from '@storybook/react-vite'
import { Bold, Code, Italic, Link as LinkIcon, Strikethrough } from 'lucide-react'
import { Input } from '../ui/input'

// Visual reference for the selection bubble-toolbar chrome. The live component
// (BubbleToolbarView) needs a Milkdown plugin-view context, so — like the
// callouts story — this renders the same DOM/classes in plain React. The
// toolbar is `position: fixed` and hidden until `data-show`, so the preview
// forces it static + shown.

function Btn({
  label,
  active,
  children,
}: {
  label: string
  active?: boolean
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      className="tela-bubble-btn"
      aria-label={label}
      aria-pressed={active}
      data-active={active ? 'true' : 'false'}
    >
      {children}
    </button>
  )
}

function Toolbar({ children }: { children: React.ReactNode }) {
  return (
    <div
      role="toolbar"
      aria-label="Format selection"
      className="tela-bubble-toolbar"
      data-show="true"
      style={{ position: 'static' }}
    >
      {children}
    </div>
  )
}

const meta: Meta = {
  title: 'App/Milkdown Bubble Toolbar',
  parameters: { layout: 'padded' },
}
export default meta

type Story = StoryObj

export const Default: Story = {
  render: () => (
    <Toolbar>
      <Btn label="Bold">
        <Bold size="1em" strokeWidth={2.5} aria-hidden />
      </Btn>
      <Btn label="Italic">
        <Italic size="1em" strokeWidth={2.5} aria-hidden />
      </Btn>
      <Btn label="Strikethrough">
        <Strikethrough size="1em" strokeWidth={2.5} aria-hidden />
      </Btn>
      <Btn label="Inline code">
        <Code size="1em" strokeWidth={2.5} aria-hidden />
      </Btn>
      <Btn label="Link">
        <LinkIcon size="1em" strokeWidth={2.5} aria-hidden />
      </Btn>
    </Toolbar>
  ),
}

export const WithActiveMarks: Story = {
  name: 'Bold + italic active',
  render: () => (
    <Toolbar>
      <Btn label="Bold" active>
        <Bold size="1em" strokeWidth={2.5} aria-hidden />
      </Btn>
      <Btn label="Italic" active>
        <Italic size="1em" strokeWidth={2.5} aria-hidden />
      </Btn>
      <Btn label="Strikethrough">
        <Strikethrough size="1em" strokeWidth={2.5} aria-hidden />
      </Btn>
      <Btn label="Inline code">
        <Code size="1em" strokeWidth={2.5} aria-hidden />
      </Btn>
      <Btn label="Link">
        <LinkIcon size="1em" strokeWidth={2.5} aria-hidden />
      </Btn>
    </Toolbar>
  ),
}

export const LinkMode: Story = {
  name: 'Link entry mode',
  render: () => (
    <Toolbar>
      <Input
        size="sm"
        type="url"
        placeholder="Paste or type a link, then Enter"
        aria-label="Link URL"
        className="tela-bubble-link-input"
        defaultValue="https://tela.cagdas.io"
      />
    </Toolbar>
  ),
}
