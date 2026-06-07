import type { Meta, StoryObj } from '@storybook/react-vite'

// Static render of the `:::stats` tile DOM (the classes the decoration plugin
// adds: tela-stat-figure / tela-stat-trend-{up,down,flat} / tela-stat-desc) so
// the mira-style hierarchy — big number + small unit, coloured trend, muted
// description — can be reviewed across themes without mounting the editor.

interface Tile {
  label: string
  num: string
  unit?: string
  trend?: string
  dir?: 'up' | 'down' | 'flat'
  desc?: string
}

function StatGrid({ tiles }: { tiles: Tile[] }) {
  return (
    <div className="tela-milkdown">
      <div className="ProseMirror">
        <div className="tela-stats">
          {tiles.map((t) => (
            <div key={t.label} className="tela-stat">
              <div className="tela-stat-head" contentEditable={false}>
                <span className="tela-stat-label">{t.label}</span>
              </div>
              <div className="tela-stat-value">
                <p className="tela-stat-figure">
                  <strong>{t.num}</strong>
                  {t.unit ? ` ${t.unit}` : ''}
                </p>
                {t.trend ? (
                  <p className={`tela-stat-trend tela-stat-trend-${t.dir ?? 'flat'}`}>
                    {t.trend}
                  </p>
                ) : null}
                {t.desc ? <p className="tela-stat-desc">{t.desc}</p> : null}
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
      { label: 'Solar PV LCOE (2024)', num: '$43', unit: '/MWh', trend: '↓ 90% since 2010', dir: 'down', desc: 'Global weighted average (IRENA)' },
      { label: 'Solar capacity (2024)', num: '1,865', unit: 'GW', trend: '↑ 10.5× vs 2014', dir: 'up', desc: 'Up from 177 GW at end-2014' },
      { label: 'Avg. response', num: '142', unit: 'ms', trend: '↓ 12%', dir: 'down', desc: 'p95 latency, last 30 days' },
      { label: 'NPS', num: '62', trend: '↑ 4 pts', dir: 'up', desc: 'vs last survey' },
    ],
  },
}
