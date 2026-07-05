import type { Meta, StoryObj } from '@storybook/react-vite'
import { EventRow, collapseEvents } from './EventRow'
import type { EventEntry } from '../../lib/types'

const meta: Meta<typeof EventRow> = {
  title: 'App/EventRow',
  component: EventRow,
}
export default meta

type Story = StoryObj<typeof EventRow>

function ev(p: Partial<EventEntry>): EventEntry {
  return {
    id: 1,
    type: 'auth.login',
    actor_user_id: 1,
    actor_label: 'alice',
    target_kind: '',
    target_id: null,
    target_label: '',
    detail: '',
    ip: '203.0.113.7',
    user_agent: 'Mozilla/5.0',
    created_at: '2026-06-11 09:30:00',
    ...p,
  }
}

const SAMPLE: EventEntry[] = [
  ev({ id: 9, type: 'auth.login', actor_label: 'alice' }),
  ev({ id: 8, type: 'auth.login_failed', actor_label: 'bob', detail: 'invalid credentials: bob' }),
  ev({ id: 7, type: 'page.view', actor_label: 'alice', target_kind: 'page', target_id: 22, target_label: 'Runbook' }),
  ev({ id: 6, type: 'page.edit', actor_label: 'carol', target_kind: 'page', target_id: 22, target_label: 'Runbook', detail: 'human' }),
  ev({ id: 5, type: 'page.create', actor_label: 'carol', target_kind: 'page', target_id: 31, target_label: 'Onboarding' }),
  ev({ id: 4, type: 'access.account.set_plan', actor_label: 'admin', detail: 'team_pro' }),
  ev({ id: 3, type: 'ask', actor_label: 'alice', detail: '"how do I deploy?" (4 hits)' }),
  ev({ id: 2, type: 'api.request', actor_label: 'mcp-bot', detail: 'GET /api/pages/22 → 200' }),
]

export const Feed: Story = {
  render: () => (
    <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)] max-w-[40rem]">
      {SAMPLE.map((e) => (
        <EventRow key={e.id} event={e} />
      ))}
    </ul>
  ),
}

// A burst of autosave edits on one page by one user collapses into a single ×N
// row spanning the run, instead of flooding the feed with duplicates.
const BURST: EventEntry[] = [
  ev({ id: 20, type: 'page.edit', actor_label: 'hazal', target_kind: 'page', target_id: 22, target_label: 'Runbook', detail: 'edit', created_at: '2026-06-11 09:30:00' }),
  ev({ id: 19, type: 'page.edit', actor_label: 'hazal', target_kind: 'page', target_id: 22, target_label: 'Runbook', detail: 'edit', created_at: '2026-06-11 09:28:00' }),
  ev({ id: 18, type: 'page.edit', actor_label: 'hazal', target_kind: 'page', target_id: 22, target_label: 'Runbook', detail: 'edit', created_at: '2026-06-11 09:12:00' }),
  ev({ id: 17, type: 'page.edit', actor_label: 'hazal', target_kind: 'page', target_id: 22, target_label: 'Runbook', detail: 'edit', created_at: '2026-06-11 08:40:00' }),
  // A different page breaks the run.
  ev({ id: 16, type: 'page.edit', actor_label: 'hazal', target_kind: 'page', target_id: 31, target_label: 'Onboarding', detail: 'edit', created_at: '2026-06-11 08:35:00' }),
]

export const CollapsedBurst: Story = {
  render: () => (
    <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)] max-w-[40rem]">
      {collapseEvents(BURST).map((g) => (
        <EventRow key={g.head.id} event={g.head} count={g.count} oldestAt={g.oldestAt} />
      ))}
    </ul>
  ),
}
