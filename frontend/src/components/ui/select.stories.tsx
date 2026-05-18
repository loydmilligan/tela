import type { Meta, StoryObj } from '@storybook/react-vite'
import { Select } from './select'

const meta: Meta<typeof Select> = {
  title: 'UI/Select',
  component: Select,
  argTypes: {
    size: { control: 'select', options: ['sm', 'md', 'lg'] },
    disabled: { control: 'boolean' },
  },
}
export default meta

type Story = StoryObj<typeof Select>

export const Basic: Story = {
  render: (args) => (
    <Select defaultValue="api" {...args}>
      <option value="">(no parent — root page)</option>
      <option value="api">API design</option>
      <option value="onboarding">Onboarding</option>
      <option value="incidents">Incidents</option>
    </Select>
  ),
}

export const Sizes: Story = {
  render: () => (
    <div className="flex flex-col gap-[var(--space-3)] w-[20rem]">
      <Select size="sm" defaultValue="api">
        <option value="api">Small select</option>
        <option value="b">Option B</option>
      </Select>
      <Select size="md" defaultValue="api">
        <option value="api">Medium select</option>
        <option value="b">Option B</option>
      </Select>
      <Select size="lg" defaultValue="api">
        <option value="api">Large select</option>
        <option value="b">Option B</option>
      </Select>
    </div>
  ),
}

export const Disabled: Story = {
  render: () => (
    <Select disabled defaultValue="api">
      <option value="api">Disabled select</option>
    </Select>
  ),
}
