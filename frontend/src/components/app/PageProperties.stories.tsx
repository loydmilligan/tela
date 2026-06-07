import type { Meta, StoryObj } from '@storybook/react-vite'
import { PageProperties } from './PageProperties'

const meta = {
  title: 'App/PageProperties',
  component: PageProperties,
  parameters: { layout: 'padded' },
} satisfies Meta<typeof PageProperties>

export default meta
type Story = StoryObj<typeof meta>

export const Mixed: Story = {
  args: {
    props: {
      owner: 'cagdas',
      priority: 3,
      draft: false,
      tags: ['infra', 'rag'],
      source: { repo: 'tela', path: 'docs/x.md' },
    },
  },
}

export const SingleScalar: Story = {
  args: { props: { status: 'imported' } },
}

// Empty bag renders nothing — pages without frontmatter are unaffected.
export const Empty: Story = {
  args: { props: {} },
}
