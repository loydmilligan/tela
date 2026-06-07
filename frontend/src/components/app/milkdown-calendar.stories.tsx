import type { Meta, StoryObj } from '@storybook/react-vite'
import { useEffect, useRef } from 'react'
import { buildGrid } from './milkdown-calendar'

// Renders the REAL month grid via the block's own `buildGrid`, so the story
// exercises the actual grid math (weekday alignment, out-of-month cells, event
// placement) and not a replica. Wrapped in `.tela-milkdown .ProseMirror` so the
// editor.css calendar rules apply, theme-driven across light/dark/warm.

function Calendar({
  month,
  events,
}: {
  month: string
  events: [string, string[]][]
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const host = ref.current
    if (!host) return
    const grid = buildGrid(month, new Map(events))
    host.replaceChildren(grid)
  }, [month, events])
  return (
    <div className="tela-milkdown">
      <div className="ProseMirror">
        <div className="tela-calendar" data-editable="false">
          <div ref={ref} />
        </div>
      </div>
    </div>
  )
}

const meta: Meta<typeof Calendar> = {
  title: 'App/Milkdown Calendar',
  component: Calendar,
  parameters: { layout: 'padded' },
}
export default meta

type Story = StoryObj<typeof Calendar>

export const LaunchMonth: Story = {
  name: 'Launch calendar',
  args: {
    month: '2026-05',
    events: [
      ['2026-05-04', ['Spec freeze']],
      ['2026-05-11', ['Beta cohort opens']],
      ['2026-05-18', ['Dogfood week']],
      ['2026-05-28', ['GA launch']],
    ],
  },
}
