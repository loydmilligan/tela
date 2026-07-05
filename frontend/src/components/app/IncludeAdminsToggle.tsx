import { Checkbox } from '../ui/checkbox'

// Shared control for the instance-admin insight surfaces (Insights, Events,
// Errors, Audit). Those screens hide instance-admin activity by default — it's
// mostly the operator's own testing noise — and this brings it back. Wired to the
// backend's ?include_admins query flag.
export function IncludeAdminsToggle({
  checked,
  onChange,
}: {
  checked: boolean
  onChange: (next: boolean) => void
}) {
  return (
    <label className="flex items-center gap-[var(--space-2)] whitespace-nowrap text-[length:var(--text-sm)] text-[var(--text-muted)] cursor-pointer select-none font-[family-name:var(--font-sans)]">
      <Checkbox
        checked={checked}
        onCheckedChange={(next) => onChange(next === true)}
      />
      <span>Include admins</span>
    </label>
  )
}
