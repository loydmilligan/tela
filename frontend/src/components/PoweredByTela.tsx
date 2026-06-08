import { useHostContext } from '../lib/queries/host-context'
import { cn } from '../lib/utils'

// A discreet product credit shown ONLY on an org's custom domain — on the
// canonical host the app is already tela-branded, so this renders nothing.
// Links to the tela product site, so every white-labeled org domain quietly
// credits (and markets) tela. Deliberately small/muted: present, not loud.
const TELA_HOME = 'https://tela.cagdas.io'

export function PoweredByTela({ className }: { className?: string }) {
  const org = useHostContext().data?.org ?? null
  if (!org) return null
  return (
    <a
      href={TELA_HOME}
      target="_blank"
      rel="noopener noreferrer"
      className={cn(
        'inline-block text-[length:var(--text-xs)] text-[var(--text-muted)]',
        'no-underline hover:text-[var(--text-primary)] transition-colors',
        className,
      )}
    >
      Powered by tela
    </a>
  )
}
