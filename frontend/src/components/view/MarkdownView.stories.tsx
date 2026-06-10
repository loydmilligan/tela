import type { Meta, StoryObj } from '@storybook/react-vite'
import { MarkdownView } from './MarkdownView'

// Rendered inside a `.tela-reader` scope (+ a centered article column) so the
// existing reading typography applies — this is how view mode will sit in the
// app/reader. See docs/view-edit-split.md.
function ReaderFrame({ children }: { children: React.ReactNode }) {
  return (
    <div className="tela-reader" data-reading-size="m" style={{ height: 'auto' }}>
      <div style={{ maxWidth: '40rem', margin: '0 auto', padding: 'var(--space-8)' }}>
        {children}
      </div>
    </div>
  )
}

const meta: Meta<typeof MarkdownView> = {
  title: 'View/MarkdownView',
  component: MarkdownView,
  decorators: [
    (Story) => (
      <ReaderFrame>
        <Story />
      </ReaderFrame>
    ),
  ],
}
export default meta

type Story = StoryObj<typeof MarkdownView>

const SAMPLE = `# Markdown view

A read-only render straight from markdown — **no editor, no collab**. It reuses
the editor's parse transforms and \`tela-*\` classes, with _emphasis_, ~~strike~~,
\`inline code\`, and a [link](https://tela.cagdas.io).

## Lists

- a bullet
- another, with \`code\`
  - nested

1. first
2. second

- [x] done task
- [ ] open task

## Callout

> [!NOTE]
> Callouts render with the same chrome + classes as the editor.

> [!WARNING]
> Including the per-type icon and label.

## Quote & highlight

> A plain blockquote.

Some ==highlighted== text inline.

## Code

\`\`\`ts
function greet(name: string): string {
  return \`hello, \${name}\`
}
\`\`\`

## Table

| Block | Status |
| :--- | :---: |
| headings | done |
| code | done |
| callout | done |

## Math

Inline $E = mc^2$ and a block:

$$
\\int_0^1 x^2 \\, dx = \\tfrac{1}{3}
$$

---

The end.
`

export const Sample: Story = {
  args: { body: SAMPLE },
}

export const Empty: Story = {
  args: { body: '' },
}

const DIAGRAMS = `## Mermaid

\`\`\`mermaid
graph TD
  A[Start] --> B{Choice}
  B -->|yes| C[Ship it]
  B -->|no| D[Iterate]
\`\`\`

## Chart

\`\`\`chart
type: bar
title: Quarterly revenue
x: [Q1, Q2, Q3, Q4]
series:
  - name: Revenue
    data: [12, 19, 15, 27]
\`\`\`
`

// mermaid renders to SVG, chart to an ECharts SVG — both via the same render
// cores the editor uses (lib/diagrams/*), lazy-loaded.
export const Diagrams: Story = {
  args: { body: DIAGRAMS },
}

const COLLAPSIBLES = `Before the fold.

<details><summary>Closed by default</summary>

Hidden **body** content with a list:

- one
- two

</details>

<details open><summary>Starts expanded</summary>

Honors the saved \`open\` attribute.

<details><summary>Nested</summary>

Inner body.

</details>

</details>

After the fold.
`

// Native <details> via the shared collapsiblesRemark transform — closed by
// default, `open` attr honored, nesting supported.
export const Collapsibles: Story = {
  args: { body: COLLAPSIBLES },
}

const BOARDS = `## Kanban

:::kanban
### To do
- [ ] Write the brief
- [ ] Collect feedback

### In progress
- [ ] Ship the view renderers

### Done
- [x] Editor blocks
:::

## Stat grid

:::stats
### Revenue
**$4.2M**

↑ 18% QoQ

vs last quarter

### Avg. response
**142** ms

↓ 12%

p95 latency

### Coverage
**100%**

→ flat

view = edit
:::

## Calendar

:::calendar{month=2026-05}
- 2026-05-04 Spec freeze
- 2026-05-18 Dogfood week
- 2026-05-28 GA launch
:::
`

// Kanban (static columns + cards), stat tiles (value/trend/desc classified by
// the shared lib/blocks/stat-trend helpers), and the calendar month grid
// (mounted from the shared lib/blocks/calendar-grid builder).
export const BoardsAndData: Story = {
  args: { body: BOARDS },
}
