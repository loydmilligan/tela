import type { Meta, StoryObj } from '@storybook/react-vite'
import { useEffect, useRef } from 'react'
import { buildChartWidget } from './milkdown-chart'

// Mounts a REAL chart through buildChartWidget (lazy ECharts + YAML), so the
// story exercises the actual render path and theme-token colours.

function Chart({ spec }: { spec: string }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const host = ref.current
    if (!host) return
    host.replaceChildren(buildChartWidget(spec))
  }, [spec])
  return (
    <div className="tela-milkdown">
      <div className="ProseMirror" style={{ maxWidth: '42rem' }}>
        <div ref={ref} />
      </div>
    </div>
  )
}

const meta: Meta<typeof Chart> = {
  title: 'App/Milkdown Chart',
  component: Chart,
  parameters: { layout: 'padded' },
}
export default meta

type Story = StoryObj<typeof Chart>

export const GroupedBar: Story = {
  name: 'Grouped bar',
  args: {
    spec: `type: grouped-bar
title: Revenue vs costs
x: [Q1, Q2, Q3, Q4]
series:
  - name: Revenue
    data: [120, 145, 180, 210]
  - name: Costs
    data: [80, 95, 110, 130]`,
  },
}

export const Line: Story = {
  args: {
    spec: `type: line
title: Weekly active users
x: [W1, W2, W3, W4, W5, W6]
series:
  - name: Users
    data: [4.2, 4.8, 5.1, 5.0, 5.6, 6.3]`,
  },
}

export const Donut: Story = {
  args: {
    spec: `type: donut
title: Browser share
data:
  - { label: Chrome, value: 64 }
  - { label: Safari, value: 20 }
  - { label: Firefox, value: 11 }
  - { label: Edge, value: 5 }`,
  },
}
