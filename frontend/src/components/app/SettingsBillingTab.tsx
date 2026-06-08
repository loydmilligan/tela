import { Check, HardDrive, Layers, Users } from 'lucide-react'
import { useMe } from '../../lib/queries/auth'
import { useOrgs } from '../../lib/queries/orgs'
import { useMyUsage, useOrgUsage, usePlans } from '../../lib/queries/billing'
import type { Plan, Usage } from '../../lib/types'
import { PlanTierSelect } from './PlanTierSelect'
import { Badge } from '../ui/badge'
import { Card } from '../ui/card'
import { Progress } from '../ui/progress'

// SettingsBillingTab — "Plan & Usage". Shows the caller's personal-account tier
// and live usage, the same for every org they belong to, and the full tier
// catalog for comparison. Instance-admins additionally get an inline tier
// selector per account (there's no self-serve billing yet — plan changes are an
// operator action).

const INFINITY = '∞'

// Capabilities every tier ships — tiers only change limits, never features.
const INCLUDED = [
  'Semantic (RAG) + full-text search',
  'MCP connector for Claude & ChatGPT',
  'Local folder sync over WebDAV',
  'Real-time multiplayer editing',
  'SSO, organizations & per-space roles',
  'Plain markdown you own — export anytime',
]

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 10 || Number.isInteger(v) ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

function formatStorageLimit(max: number | null): string {
  return max == null ? INFINITY : formatBytes(max)
}

function formatCount(max: number | null): string {
  return max == null ? INFINITY : String(max)
}

interface MetricProps {
  icon: React.ReactNode
  label: string
  used: number
  max: number | null
  format?: (n: number) => string
}

function Metric({ icon, label, used, max, format }: MetricProps) {
  const fmt = format ?? String
  const limit = max == null ? INFINITY : fmt(max)
  return (
    <div className="flex flex-col gap-[var(--space-2)]">
      <div className="flex items-center justify-between gap-[var(--space-2)]">
        <span className="inline-flex items-center gap-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
          <span aria-hidden className="text-[var(--text-muted)]">
            {icon}
          </span>
          {label}
        </span>
        <span className="text-[length:var(--text-sm)] text-[var(--text-primary)] tabular-nums">
          {fmt(used)} <span className="text-[var(--text-muted)]">/ {limit}</span>
        </span>
      </div>
      <Progress value={used} max={max} />
    </div>
  )
}

// One account's plan + usage. account identifies who to re-plan (admin only).
function UsageCard({
  title,
  subtitle,
  usage,
  isPending,
  isError,
  account,
}: {
  title: string
  subtitle?: string
  usage: Usage | undefined
  isPending: boolean
  isError: boolean
  account?: { kind: 'user' | 'org'; id: number } | null
}) {
  return (
    <Card className="flex flex-col gap-[var(--space-4)] p-[var(--space-5)]">
      <div className="flex items-start justify-between gap-[var(--space-3)]">
        <div className="min-w-0">
          <h3 className="m-0 font-medium text-[var(--text-primary)] truncate">{title}</h3>
          {subtitle ? (
            <p className="m-0 mt-[var(--space-1)] text-[length:var(--text-xs)] text-[var(--text-muted)]">
              {subtitle}
            </p>
          ) : null}
        </div>
        {usage ? (
          <Badge variant="accent" className="shrink-0">
            {usage.plan.name}
          </Badge>
        ) : null}
      </div>

      {isPending ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">Loading usage…</p>
      ) : isError || !usage ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
          Couldn't load usage.
        </p>
      ) : (
        <div className="flex flex-col gap-[var(--space-4)]">
          <Metric
            icon={<Layers width={15} height={15} />}
            label="Spaces"
            used={usage.usage.spaces}
            max={usage.plan.max_spaces}
          />
          <Metric
            icon={<HardDrive width={15} height={15} />}
            label="Storage"
            used={usage.usage.storage_bytes}
            max={usage.plan.max_storage_bytes}
            format={formatBytes}
          />
          {usage.account_kind === 'org' ? (
            <Metric
              icon={<Users width={15} height={15} />}
              label="Members"
              used={usage.usage.members ?? 0}
              max={usage.plan.max_members}
            />
          ) : null}
        </div>
      )}

      {account && usage ? (
        <div className="flex items-center gap-[var(--space-2)] border-t border-[var(--border-subtle)] pt-[var(--space-3)]">
          <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">
            Admin · set tier
          </span>
          <PlanTierSelect
            accountKind={account.kind}
            accountId={account.id}
            currentKey={usage.plan.key}
            className="max-w-[12rem]"
          />
        </div>
      ) : null}
    </Card>
  )
}

// A compact spec line for a plan in the comparison grid.
function planSpecs(p: Plan): string[] {
  const specs = [
    `${formatCount(p.max_spaces)} spaces`,
    `${p.max_pages_per_space == null ? INFINITY : p.max_pages_per_space} pages / space`,
    `${formatStorageLimit(p.max_storage_bytes)} storage`,
  ]
  if (p.account_kind === 'org') specs.push(`${formatCount(p.max_members)} members`)
  return specs
}

function PlanCatalog({ plans, currentKey }: { plans: Plan[]; currentKey: string | undefined }) {
  const groups: { kind: 'user' | 'org'; label: string }[] = [
    { kind: 'user', label: 'Personal' },
    { kind: 'org', label: 'Organization' },
  ]
  return (
    <div className="flex flex-col gap-[var(--space-5)]">
      {groups.map((g) => {
        const tiers = plans.filter((p) => p.account_kind === g.kind && p.listed)
        if (tiers.length === 0) return null
        return (
          <div key={g.kind} className="flex flex-col gap-[var(--space-3)]">
            <h4 className="m-0 text-[length:var(--text-xs)] uppercase tracking-[0.08em] text-[var(--text-muted)]">
              {g.label} plans
            </h4>
            <div className="grid gap-[var(--space-3)] sm:grid-cols-2 lg:grid-cols-3">
              {tiers.map((p) => {
                const isCurrent = p.key === currentKey
                return (
                  <Card
                    key={p.key}
                    className="flex flex-col gap-[var(--space-3)] p-[var(--space-4)]"
                    style={
                      isCurrent
                        ? { borderColor: 'var(--accent)' }
                        : undefined
                    }
                  >
                    <div className="flex items-center justify-between gap-[var(--space-2)]">
                      <span className="font-medium text-[var(--text-primary)]">{p.name}</span>
                      {isCurrent ? <Badge variant="accent">Current</Badge> : null}
                    </div>
                    <ul className="m-0 flex list-none flex-col gap-[var(--space-1)] p-0">
                      {planSpecs(p).map((s) => (
                        <li
                          key={s}
                          className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
                        >
                          {s}
                        </li>
                      ))}
                    </ul>
                  </Card>
                )
              })}
            </div>
          </div>
        )
      })}
    </div>
  )
}

export function SettingsBillingTab() {
  const me = useMe()
  const orgs = useOrgs()
  const myUsage = useMyUsage()
  const plans = usePlans()
  const isAdmin = me.data?.is_instance_admin ?? false
  // Orgs the caller actually belongs to (instance-admins see all orgs via the
  // list, but usage cards make sense only for orgs they're a member of).
  const myOrgs = (orgs.data ?? []).filter((o) => o.my_role != null)

  return (
    <div className="flex flex-col gap-[var(--space-6)]">
      <header className="flex flex-col gap-[var(--space-1)]">
        <h2 className="m-0 text-[length:var(--text-lg)] font-medium text-[var(--text-primary)]">
          Plan &amp; Usage
        </h2>
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] max-w-[var(--measure,60ch)]">
          Your personal account and each organization you belong to carry their own
          tier. Limits apply to whoever owns a space.
        </p>
      </header>

      <UsageCard
        title="Personal account"
        subtitle={me.data?.username}
        usage={myUsage.data}
        isPending={myUsage.isPending}
        isError={myUsage.isError}
        account={isAdmin && me.data ? { kind: 'user', id: me.data.id } : null}
      />

      {myOrgs.length > 0 ? (
        <div className="flex flex-col gap-[var(--space-3)]">
          <h3 className="m-0 text-[length:var(--text-xs)] uppercase tracking-[0.08em] text-[var(--text-muted)]">
            Organizations
          </h3>
          <div className="flex flex-col gap-[var(--space-3)]">
            {myOrgs.map((o) => (
              <OrgUsageCard key={o.id} orgId={o.id} name={o.name} isAdmin={isAdmin} />
            ))}
          </div>
        </div>
      ) : null}

      {plans.data && plans.data.length > 0 ? (
        <section className="flex flex-col gap-[var(--space-3)]">
          <h3 className="m-0 text-[length:var(--text-base)] font-medium text-[var(--text-primary)]">
            Tiers
          </h3>
          <PlanCatalog plans={plans.data} currentKey={myUsage.data?.plan.key} />
        </section>
      ) : null}

      <section className="flex flex-col gap-[var(--space-3)]">
        <h3 className="m-0 text-[length:var(--text-xs)] uppercase tracking-[0.08em] text-[var(--text-muted)]">
          Every plan includes
        </h3>
        <ul className="m-0 grid list-none grid-cols-1 gap-[var(--space-2)] p-0 sm:grid-cols-2">
          {INCLUDED.map((f) => (
            <li
              key={f}
              className="flex items-start gap-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-muted)]"
            >
              <Check
                width={15}
                height={15}
                aria-hidden
                className="mt-[2px] shrink-0 text-[var(--accent)]"
              />
              {f}
            </li>
          ))}
        </ul>
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          Prefer to run it yourself? tela is open source and self-hostable —{' '}
          <a
            href="https://github.com/zcag/tela"
            target="_blank"
            rel="noopener noreferrer"
            className="text-[var(--accent)] no-underline hover:underline"
          >
            self-host it
          </a>
          .
        </p>
      </section>
    </div>
  )
}

// Split out so each org's usage query runs under hooks rules (one hook per card).
function OrgUsageCard({
  orgId,
  name,
  isAdmin,
}: {
  orgId: number
  name: string
  isAdmin: boolean
}) {
  const usage = useOrgUsage(orgId)
  return (
    <UsageCard
      title={name}
      subtitle="Organization"
      usage={usage.data}
      isPending={usage.isPending}
      isError={usage.isError}
      account={isAdmin ? { kind: 'org', id: orgId } : null}
    />
  )
}
