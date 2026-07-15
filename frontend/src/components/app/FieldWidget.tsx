import { cn } from '../../lib/utils'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import type { FieldSpec } from '../../lib/blocks/field-widget'

// FieldWidget — the reader-side surface of a ` ```field ` block. Presentational:
// it renders the current value of the bound prop and calls `onCommit` with the
// new value on interaction; persistence (PATCH /api/pages/{id}/props, then a
// page-detail invalidation) is the caller's job (FieldBlockView in MarkdownView).
// Tokens + owned primitives only — the segmented control is a local token
// element (there is no owned segmented primitive), mirroring how PollWidget
// builds its bespoke result rows.
//
// canEdit gates whether interaction is allowed: on the app read view an editor
// can flip a field; on public/share surfaces (and for viewers) it renders the
// current value read-only. The backend is still the authority.

export interface FieldWidgetProps {
  spec: FieldSpec
  /** Current value from the page's props[spec.prop]. */
  value: unknown
  /** Whether the caller may write (editor on the app read view). */
  canEdit: boolean
  /** Fired with the new value to persist. */
  onCommit: (value: unknown) => void
  /** A write is in flight — controls are disabled. */
  pending?: boolean
  className?: string
}

function asString(v: unknown): string {
  if (v == null) return ''
  if (typeof v === 'string') return v
  return String(v)
}

// A single-select segmented control. `null`-safe: when no option matches, none
// is active (a fresh, unset field).
function Segmented({
  options,
  active,
  disabled,
  onPick,
}: {
  options: { value: string; label: string }[]
  active: string | null
  disabled: boolean
  onPick: (value: string) => void
}) {
  return (
    <div
      role="radiogroup"
      className="inline-flex flex-wrap gap-[calc(var(--space-1)/2)] rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-1)] p-[calc(var(--space-1)/2)]"
    >
      {options.map((o) => {
        const isActive = active === o.value
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={isActive}
            disabled={disabled}
            onClick={() => onPick(o.value)}
            className={cn(
              'rounded-[var(--radius-sm)] px-[var(--space-3)] py-[var(--space-1)] text-[length:var(--text-sm)] transition-colors',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-[var(--accent)]',
              'disabled:cursor-not-allowed',
              isActive
                ? 'bg-[var(--accent)] text-[var(--accent-fg)]'
                : 'text-[var(--text-muted)] enabled:hover:bg-[var(--surface-2)] enabled:hover:text-[var(--text-primary)]',
            )}
          >
            {o.label}
          </button>
        )
      })}
    </div>
  )
}

function FieldControl({ spec, value, canEdit, onCommit, pending }: FieldWidgetProps) {
  const disabled = !canEdit || !!pending

  switch (spec.type) {
    case 'select':
      return (
        <Segmented
          options={spec.options.map((o) => ({ value: o, label: o }))}
          active={spec.options.includes(asString(value)) ? asString(value) : null}
          disabled={disabled}
          onPick={(v) => onCommit(v)}
        />
      )

    case 'toggle':
      return (
        <Segmented
          options={[
            { value: 'false', label: 'Off' },
            { value: 'true', label: 'On' },
          ]}
          active={value === true ? 'true' : value === false ? 'false' : null}
          disabled={disabled}
          onPick={(v) => onCommit(v === 'true')}
        />
      )

    case 'button':
      return (
        <div className="flex items-center gap-[var(--space-2)]">
          <Button
            size="sm"
            disabled={disabled}
            onClick={() => onCommit(spec.value)}
          >
            {spec.label || spec.value}
          </Button>
          {asString(value) !== '' && (
            <Badge variant={asString(value) === spec.value ? 'accent' : 'muted'}>
              {asString(value)}
            </Badge>
          )}
        </div>
      )

    case 'text':
    default:
      return <TextField value={value} disabled={disabled} onCommit={onCommit} />
  }
}

// Text input — uncontrolled, keyed by the committed value so a successful write
// (props change → re-render) re-seeds it, while typing between commits isn't
// clobbered by re-renders. Commits on blur / Enter, only when the value changed.
function TextField({
  value,
  disabled,
  onCommit,
}: {
  value: unknown
  disabled: boolean
  onCommit: (value: unknown) => void
}) {
  const committed = asString(value)
  return (
    <Input
      key={committed}
      defaultValue={committed}
      disabled={disabled}
      onBlur={(e) => {
        if (e.currentTarget.value !== committed) onCommit(e.currentTarget.value)
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault()
          e.currentTarget.blur()
        }
      }}
      className="max-w-[calc(var(--space-8)*5)]"
    />
  )
}

export function FieldWidget(props: FieldWidgetProps) {
  const { spec, className } = props
  return (
    <div
      className={cn(
        'my-[var(--space-3)] flex flex-col gap-[var(--space-2)] rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-1)] p-[var(--space-3)]',
        className,
      )}
      data-field-prop={spec.prop}
    >
      <span className="text-[length:var(--text-xs)] font-medium uppercase tracking-wide text-[var(--text-muted)]">
        {spec.label || spec.prop}
      </span>
      <FieldControl {...props} />
    </div>
  )
}
