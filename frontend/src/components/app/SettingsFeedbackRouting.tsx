import { useState } from 'react'
import { Trash2 } from 'lucide-react'
import {
  useCreateFeedbackGroup,
  useDeleteFeedbackGroup,
  useFeedbackAdminSettings,
  useUpdateFeedbackGroup,
  useUpdateFeedbackPermission,
  useUpdateFeedbackSettings,
} from '../../lib/queries/feedback'
import type { FeedbackGroup, FeedbackPermission } from '../../lib/types'
import { Button } from '../ui/button'
import { Checkbox } from '../ui/checkbox'
import { Input } from '../ui/input'
import { Select } from '../ui/select'

// Feedback → Settings pane (phase 2a): the composer's default receiver,
// receiver groups (a group message is claimed by whoever answers first), and
// the per-user permission matrix — who may use the widget at all, and who may
// request an automated Claude triage.
export function SettingsFeedbackRouting() {
  const settings = useFeedbackAdminSettings()
  if (settings.isLoading) {
    return <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">Loading…</p>
  }
  if (settings.isError || !settings.data) {
    return (
      <p role="alert" className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
        Couldn't load feedback settings.
      </p>
    )
  }
  const s = settings.data
  return (
    <div className="flex flex-col gap-[var(--space-6)]">
      <DefaultReceiver
        users={s.permissions}
        groups={s.groups}
        defaultUserId={s.default_user_id}
        defaultGroupId={s.default_group_id}
      />
      <GroupsEditor groups={s.groups} users={s.permissions} />
      <PermissionMatrix permissions={s.permissions} />
    </div>
  )
}

function SectionTitle({ title, hint }: { title: string; hint: string }) {
  return (
    <div className="flex flex-col gap-[var(--space-1)]">
      <h3 className="m-0 text-[length:var(--text-sm)] font-semibold text-[var(--text-primary)]">
        {title}
      </h3>
      <p className="m-0 text-[length:var(--text-xs)] text-[var(--text-muted)]">{hint}</p>
    </div>
  )
}

function DefaultReceiver({
  users,
  groups,
  defaultUserId,
  defaultGroupId,
}: {
  users: FeedbackPermission[]
  groups: FeedbackGroup[]
  defaultUserId: number | null
  defaultGroupId: number | null
}) {
  const update = useUpdateFeedbackSettings()
  const value =
    defaultGroupId != null ? `g:${defaultGroupId}` : defaultUserId != null ? `u:${defaultUserId}` : ''
  return (
    <section className="flex flex-col gap-[var(--space-3)]">
      <SectionTitle
        title="Default receiver"
        hint="Pre-selected target in the composer. Senders can still pick anyone."
      />
      <Select
        aria-label="Default receiver"
        value={value}
        onChange={(e) => {
          const v = e.target.value
          if (v === '') void update.mutate({ default_user_id: null, default_group_id: null })
          else if (v.startsWith('g:')) void update.mutate({ default_group_id: Number(v.slice(2)) })
          else void update.mutate({ default_user_id: Number(v.slice(2)) })
        }}
        className="max-w-[18rem]"
      >
        <option value="">No default</option>
        {groups.map((g) => (
          <option key={`g${g.id}`} value={`g:${g.id}`}>
            Group: {g.name}
          </option>
        ))}
        {users.map((u) => (
          <option key={`u${u.user_id}`} value={`u:${u.user_id}`}>
            {u.username}
          </option>
        ))}
      </Select>
    </section>
  )
}

function GroupsEditor({
  groups,
  users,
}: {
  groups: FeedbackGroup[]
  users: FeedbackPermission[]
}) {
  const create = useCreateFeedbackGroup()
  const [name, setName] = useState('')
  return (
    <section className="flex flex-col gap-[var(--space-3)]">
      <SectionTitle
        title="Receiver groups"
        hint="A message to a group goes to every member; the first to see it claims and answers."
      />
      {groups.length === 0 ? (
        <p className="m-0 text-[length:var(--text-xs)] text-[var(--text-muted)]">No groups yet.</p>
      ) : (
        <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-2)]">
          {groups.map((g) => (
            <GroupRow key={g.id} group={g} users={users} />
          ))}
        </ul>
      )}
      <form
        className="flex gap-[var(--space-2)] items-center"
        onSubmit={(e) => {
          e.preventDefault()
          const n = name.trim()
          if (!n) return
          create.mutate({ name: n, member_ids: [] }, { onSuccess: () => setName('') })
        }}
      >
        <Input
          size="sm"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="New group name"
          aria-label="New group name"
          className="max-w-[14rem]"
        />
        <Button type="submit" size="sm" variant="secondary" disabled={!name.trim()}>
          Add group
        </Button>
      </form>
    </section>
  )
}

function GroupRow({ group, users }: { group: FeedbackGroup; users: FeedbackPermission[] }) {
  const update = useUpdateFeedbackGroup()
  const del = useDeleteFeedbackGroup()
  const toggleMember = (userId: number, on: boolean) => {
    const next = on
      ? [...group.member_ids, userId]
      : group.member_ids.filter((id) => id !== userId)
    update.mutate({ id: group.id, member_ids: next })
  }
  return (
    <li className="flex flex-col gap-[var(--space-2)] rounded-[var(--radius-md)] border border-[var(--border-subtle)] p-[var(--space-3)]">
      <div className="flex items-center justify-between">
        <span className="text-[length:var(--text-sm)] font-medium text-[var(--text-primary)]">
          {group.name}
        </span>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          aria-label={`Delete group ${group.name}`}
          onClick={() => del.mutate(group.id)}
        >
          <Trash2 width={14} height={14} aria-hidden />
        </Button>
      </div>
      <div className="flex flex-wrap gap-x-[var(--space-4)] gap-y-[var(--space-1)]">
        {users.map((u) => (
          <label
            key={u.user_id}
            className="inline-flex items-center gap-[var(--space-2)] text-[length:var(--text-xs)] text-[var(--text-muted)]"
          >
            <Checkbox
              checked={group.member_ids.includes(u.user_id)}
              onCheckedChange={(v) => toggleMember(u.user_id, v === true)}
              aria-label={`${u.username} in ${group.name}`}
            />
            {u.username}
          </label>
        ))}
      </div>
    </li>
  )
}

function PermissionMatrix({ permissions }: { permissions: FeedbackPermission[] }) {
  const update = useUpdateFeedbackPermission()
  return (
    <section className="flex flex-col gap-[var(--space-3)]">
      <SectionTitle
        title="Who can use feedback"
        hint="Turn the widget off per user, and grant who may request a Claude triage on their reports."
      />
      <table className="w-full max-w-[28rem] text-[length:var(--text-sm)]">
        <thead>
          <tr className="text-left text-[length:var(--text-xs)] text-[var(--text-muted)]">
            <th className="font-medium py-[var(--space-1)]">User</th>
            <th className="font-medium py-[var(--space-1)]">Can send</th>
            <th className="font-medium py-[var(--space-1)]">May request Claude</th>
          </tr>
        </thead>
        <tbody>
          {permissions.map((p) => (
            <tr key={p.user_id} className="border-t border-[var(--border-subtle)]">
              <td className="py-[var(--space-2)] text-[var(--text-primary)]">{p.username}</td>
              <td className="py-[var(--space-2)]">
                <Checkbox
                  checked={p.enabled}
                  onCheckedChange={(v) =>
                    update.mutate({ user_id: p.user_id, enabled: v === true, allow_claude: p.allow_claude })
                  }
                  aria-label={`${p.username} can send feedback`}
                />
              </td>
              <td className="py-[var(--space-2)]">
                <Checkbox
                  checked={p.allow_claude}
                  onCheckedChange={(v) =>
                    update.mutate({ user_id: p.user_id, enabled: p.enabled, allow_claude: v === true })
                  }
                  aria-label={`${p.username} may request Claude triage`}
                />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
