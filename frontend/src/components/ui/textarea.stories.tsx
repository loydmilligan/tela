import type { Meta, StoryObj } from '@storybook/react-vite'
import { TextArea } from './textarea'

const meta: Meta<typeof TextArea> = {
  title: 'UI/TextArea',
  component: TextArea,
  argTypes: {
    size: { control: 'select', options: ['sm', 'md', 'lg'] },
    font: { control: 'select', options: ['sans', 'mono'] },
    disabled: { control: 'boolean' },
  },
}
export default meta

type Story = StoryObj<typeof TextArea>

export const Basic: Story = {
  args: {
    defaultValue:
      'Markdown body goes here. Plain textarea for v0; Milkdown lands in M3.',
    placeholder: 'Start writing…',
  },
}

export const SansFont: Story = {
  args: { font: 'sans', defaultValue: 'Sans-serif body for prose pages.' },
}

export const Sizes: Story = {
  render: () => (
    <div className="flex flex-col gap-[var(--space-3)] w-[40rem]">
      <TextArea size="sm" placeholder="Small" />
      <TextArea size="md" placeholder="Medium" />
      <TextArea size="lg" placeholder="Large" />
    </div>
  ),
}

export const Disabled: Story = {
  args: { disabled: true, defaultValue: 'You cannot edit this.' },
}
