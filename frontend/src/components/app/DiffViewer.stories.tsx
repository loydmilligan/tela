import type { Meta, StoryObj } from '@storybook/react-vite'
import { DiffViewer } from './DiffViewer'

// Realistic markdown fixtures (~10–30 lines each). Pair OLD vs NEW so each
// story exercises one of: mixed changes, only-additions, only-deletions,
// no-changes, and inline-mode-default.

const OLD_MIXED = `# Onboarding

Welcome to the Engineering wiki. Edit pages with Markdown.

## Day one
- Check email
- Set up laptop
- Read the on-call rotation

## Useful links
- [[Architecture]]
- [[Tooling]]
`

const NEW_MIXED = `# Onboarding

Welcome to the Engineering wiki. Edit pages with Markdown. We snapshot
every save so you can roll back at any time.

## Day one
- Check email and respond to invitations
- Set up laptop using the bootstrap script
- Read the on-call rotation
- Schedule a 1:1 with your manager

## Useful links
- [[Architecture]]
- [[Tooling]]
- [[Code review]]
`

const OLD_ADDITIONS = `# Release notes

## 0.1.0
- Initial release
`

const NEW_ADDITIONS = `# Release notes

## 0.2.0
- Added page history with diff viewer
- Added comments panel
- Wired live-collab presence

## 0.1.0
- Initial release
`

const OLD_DELETIONS = `# Deprecated rituals

## Weekly demo
We hold a 60 minute demo on Fridays.

## Quarterly OKR review
Each team owner walks through their OKRs.

## All-hands
Monday 9am, recorded.

## Sprint planning
Two-week cadence.
`

const NEW_DELETIONS = `# Deprecated rituals

## All-hands
Monday 9am, recorded.

## Sprint planning
Two-week cadence.
`

const SAME_BODY = `# Stable doc

This page has not changed in months. Renaming intentionally avoided to
preserve external bookmarks.

## Section A
The content of section A.

## Section B
The content of section B.
`

const meta: Meta<typeof DiffViewer> = {
  title: 'App/DiffViewer',
  component: DiffViewer,
  parameters: {
    layout: 'padded',
  },
  argTypes: {
    defaultMode: { control: 'inline-radio', options: ['split', 'inline'] },
  },
}
export default meta

type Story = StoryObj<typeof DiffViewer>

export const Default: Story = {
  name: 'Default — mixed adds/dels (split)',
  args: {
    oldBody: OLD_MIXED,
    newBody: NEW_MIXED,
    oldLabel: 'Revision #5',
    newLabel: 'Current',
  },
}

export const OnlyAdditions: Story = {
  name: 'Only additions',
  args: {
    oldBody: OLD_ADDITIONS,
    newBody: NEW_ADDITIONS,
    oldLabel: 'Revision #2',
    newLabel: 'Current',
  },
}

export const OnlyDeletions: Story = {
  name: 'Only deletions',
  args: {
    oldBody: OLD_DELETIONS,
    newBody: NEW_DELETIONS,
    oldLabel: 'Revision #1',
    newLabel: 'Current',
  },
}

export const NoChanges: Story = {
  name: 'No changes — empty state',
  args: {
    oldBody: SAME_BODY,
    newBody: SAME_BODY,
    oldLabel: 'Revision #3',
    newLabel: 'Current',
  },
}

export const Inline: Story = {
  name: 'Inline mode (defaultMode=inline)',
  args: {
    oldBody: OLD_MIXED,
    newBody: NEW_MIXED,
    oldLabel: 'Revision #5',
    newLabel: 'Current',
    defaultMode: 'inline',
  },
}

// One story per theme — wraps the diff viewer in a `[data-theme]` div so the
// token palette is visually checked against each theme's surface palette.
// The component itself doesn't know about themes; this story exists so a
// reviewer can flip through Storybook and see the three skins side by side.

export const ThemeLight: Story = {
  name: 'Theme — light',
  render: (args) => (
    <div
      data-theme="light"
      className="p-[var(--space-6)] bg-[var(--surface-1)] text-[var(--text-primary)]"
    >
      <DiffViewer {...args} />
    </div>
  ),
  args: {
    oldBody: OLD_MIXED,
    newBody: NEW_MIXED,
    oldLabel: 'Revision #5',
    newLabel: 'Current',
  },
}

export const ThemeWarm: Story = {
  name: 'Theme — warm',
  render: (args) => (
    <div
      data-theme="warm"
      className="p-[var(--space-6)] bg-[var(--surface-1)] text-[var(--text-primary)]"
    >
      <DiffViewer {...args} />
    </div>
  ),
  args: {
    oldBody: OLD_MIXED,
    newBody: NEW_MIXED,
    oldLabel: 'Revision #5',
    newLabel: 'Current',
  },
}

export const ThemeDark: Story = {
  name: 'Theme — dark',
  render: (args) => (
    <div
      data-theme="dark"
      className="p-[var(--space-6)] bg-[var(--surface-1)] text-[var(--text-primary)]"
    >
      <DiffViewer {...args} />
    </div>
  ),
  args: {
    oldBody: OLD_MIXED,
    newBody: NEW_MIXED,
    oldLabel: 'Revision #5',
    newLabel: 'Current',
  },
}
