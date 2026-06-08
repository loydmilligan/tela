import { usePlans, useSetPlan } from '../../lib/queries/billing'
import { cn } from '../../lib/utils'
import type { SelectProps } from '../ui/select'
import { Select } from '../ui/select'

// PlanTierSelect — instance-admin control to set an account's tier. Reused by
// the Plan & Usage panel and the admin Users / Organizations tabs so there's one
// implementation of "change this account's plan". Lists every tier matching the
// account kind (including unlisted comp tiers, which admins may grant).
export function PlanTierSelect({
  accountKind,
  accountId,
  currentKey,
  size = 'sm',
  className,
}: {
  accountKind: 'user' | 'org'
  accountId: number
  currentKey: string
  size?: SelectProps['size']
  className?: string
}) {
  const plans = usePlans()
  const setPlan = useSetPlan()
  const options = (plans.data ?? []).filter((p) => p.account_kind === accountKind)
  if (options.length === 0) return null
  // The Select primitive wraps its <select> in a w-full span, so a width must
  // constrain THIS wrapper — not the inner element — or it blows out a flex row.
  return (
    <div className={cn('shrink-0', className)}>
      <Select
        size={size}
        value={currentKey}
        disabled={setPlan.isPending}
        onChange={(e) =>
          setPlan.mutate({
            account_kind: accountKind,
            account_id: accountId,
            plan_key: e.target.value,
          })
        }
        aria-label="Set plan tier"
      >
        {options.map((p) => (
          <option key={p.key} value={p.key}>
            {p.name}
            {p.listed ? '' : ' (internal)'}
          </option>
        ))}
      </Select>
    </div>
  )
}
