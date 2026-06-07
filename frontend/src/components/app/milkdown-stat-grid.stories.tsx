import type { Meta, StoryObj } from '@storybook/react-vite'

// Static render of the `:::stats` block DOM (the same classes the nodeView
// emits) so the tile look can be reviewed across themes without mounting the
// editor. The accent rail follows [data-accent], which the live nodeView sets
// from the trend glyph in the value.

type Accent = 'default' | 'positive' | 'negative'

interface Tile {
  label: string
  value: string
  accent?: Accent
}

function StatGrid({ tiles }: { tiles: Tile[] }) {
  return (
    <div className="tela-milkdown">
      <div className="ProseMirror">
        <div className="tela-stats">
          {tiles.map((t) => (
            <div
              key={t.label}
              className="tela-stat"
              data-accent={t.accent ?? 'default'}
            >
              <div className="tela-stat-head" contentEditable={false}>
                <span className="tela-stat-label">{t.label}</span>
              </div>
              <div className="tela-stat-value">
                <p dangerouslySetInnerHTML={{ __html: t.value }} />
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

const meta: Meta<typeof StatGrid> = {
  title: 'App/Milkdown Stat Grid',
  component: StatGrid,
  parameters: { layout: 'padded' },
}
export default meta

type Story = StoryObj<typeof StatGrid>

export const Dashboard: Story = {
  name: 'KPI dashboard',
  args: {
    tiles: [
      { label: 'Revenue', value: '<strong>$4.2M</strong> ↑ 18%', accent: 'positive' },
      { label: 'Active users', value: '<strong>12,400</strong> ↑ 3%', accent: 'positive' },
      { label: 'Churn', value: '<strong>1.8%</strong> ↓ 0.4%', accent: 'negative' },
      { label: 'NPS', value: '<strong>62</strong>', accent: 'default' },
    ],
  },
}

export const TwoUp: Story = {
  name: 'Two tiles',
  args: {
    tiles: [
      { label: 'Open issues', value: '<strong>37</strong> ↓ 12', accent: 'negative' },
      { label: 'Deploys this week', value: '<strong>9</strong> ↑ 2', accent: 'positive' },
    ],
  },
}
