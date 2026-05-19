import type { Meta, StoryObj } from '@storybook/react-vite'
import { SearchResult } from './SearchResult'

const meta: Meta<typeof SearchResult> = {
  title: 'App/SearchResult',
  component: SearchResult,
  parameters: {
    layout: 'padded',
  },
}
export default meta

type Story = StoryObj<typeof SearchResult>

// Pre-format a "minutes ago" timestamp by anchoring to a recent UTC instant.
// Storybook fixture only — picks a value that always renders as "minutes ago"
// regardless of when the story is viewed.
function recentUtc(offsetMinutes: number): string {
  const t = new Date(Date.now() - offsetMinutes * 60_000)
  return t.toISOString().replace('T', ' ').slice(0, 19)
}

export const WithExcerpt: Story = {
  name: 'With query + excerpt',
  args: {
    title: 'On-call runbook',
    breadcrumb: 'Engineering',
    excerpt:
      '…rotate the primary pager weekly; the secondary is responsible for paging the on-call manager if the primary is unreachable after 15 minutes…',
    updatedAt: recentUtc(12),
    onSelect: () => {},
  },
}

export const LongTitle: Story = {
  name: 'Long title — truncates',
  args: {
    title:
      'A very long page title that should truncate cleanly on the row instead of wrapping under the timestamp',
    breadcrumb: 'Operations / Vendors / AWS',
    excerpt:
      '…credentials rotate every 90 days. The break-glass account stays on a hardware token in the safe; do not check it out unless the rotation pipeline is down…',
    updatedAt: recentUtc(60 * 26),
    onSelect: () => {},
  },
}

// Empty-state placeholder rendered alongside the list when there are no
// results. The SearchResult component itself doesn't render anything in this
// case — this story wraps a sibling placeholder to document the page-level
// empty look (text + token-driven muted color).
export const EmptyListPlaceholder: Story = {
  name: 'Empty list — placeholder copy',
  render: () => (
    <div className="flex flex-col gap-[var(--space-4)] max-w-[40rem]">
      <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
        Type to search across all your spaces.
      </p>
    </div>
  ),
}
