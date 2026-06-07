import type { Meta, StoryObj } from '@storybook/react-vite'

// Static render of the `:::timeline` block DOM (directive wrapper + bullet list)
// so the rail / dot / date-chip treatment can be reviewed across themes. The
// live block is just this list inside a `.tela-timeline`; all visual treatment
// is the CSS over the nested list.

interface Event {
  date: string
  text: string
}

function Timeline({ events }: { events: Event[] }) {
  return (
    <div className="tela-milkdown">
      <div className="ProseMirror">
        <div className="tela-timeline">
          <ul>
            {events.map((e) => (
              <li key={e.date}>
                <p>
                  <strong>{e.date}</strong> {e.text}
                </p>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </div>
  )
}

const meta: Meta<typeof Timeline> = {
  title: 'App/Milkdown Timeline',
  component: Timeline,
  parameters: { layout: 'padded' },
}
export default meta

type Story = StoryObj<typeof Timeline>

export const Roadmap: Story = {
  name: 'Release history',
  args: {
    events: [
      { date: '2026-01-15', text: 'v1.0 shipped — first stable release' },
      { date: '2026-03-01', text: 'v1.1 — search + exports' },
      { date: '2026-04-20', text: 'v1.2 — public spaces' },
      { date: '2026-06-01', text: 'v2.0 — planned' },
    ],
  },
}
