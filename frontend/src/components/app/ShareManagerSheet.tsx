import {
  useCreateShare,
  useRevokeShare,
  useSharesForPage,
  useUpdateShare,
} from '../../lib/queries/share'
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '../ui/sheet'
import { CreateShareForm } from './ShareManagerSheet-create-form'
import { ShareRow } from './ShareManagerSheet-row'
import { shareErrorMessage } from './ShareManagerSheet-utils'

interface ShareManagerSheetProps {
  pageId: number
  open: boolean
  onOpenChange: (next: boolean) => void
}

export function ShareManagerSheet({
  pageId,
  open,
  onOpenChange,
}: ShareManagerSheetProps) {
  const sharesQuery = useSharesForPage(pageId)
  const createShare = useCreateShare(pageId)
  const updateShare = useUpdateShare()
  const revokeShare = useRevokeShare()

  // Active shares only — the backend default already filters revoked, but we
  // also drop any in-flight `revoked_at` to keep the row from flickering
  // before invalidation lands.
  const shares = (sharesQuery.data ?? []).filter((s) => !s.revoked_at)

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex flex-col">
        <SheetHeader>
          <SheetTitle>Share this page</SheetTitle>
          <SheetDescription>
            Anyone with the link can read this page. Optional password and
            subtree options below.
          </SheetDescription>
        </SheetHeader>

        <SheetBody className="flex flex-col gap-[var(--space-5)]">
          <section
            aria-labelledby={`share-active-${pageId}`}
            className="flex flex-col gap-[var(--space-3)]"
          >
            <h3
              id={`share-active-${pageId}`}
              className="m-0 text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
            >
              Active shares
            </h3>
            {sharesQuery.isLoading ? (
              <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
                Loading…
              </p>
            ) : sharesQuery.isError ? (
              <p
                role="alert"
                className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
              >
                {shareErrorMessage(sharesQuery.error)}
              </p>
            ) : shares.length === 0 ? (
              <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
                No active shares yet. Create one below.
              </p>
            ) : (
              <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-3)]">
                {shares.map((share) => (
                  <li key={share.id} className="m-0 p-0 list-none">
                    <ShareRow
                      share={share}
                      onUpdate={(patch) =>
                        updateShare.mutateAsync({
                          id: share.id,
                          pageId,
                          patch,
                        })
                      }
                      onRevoke={() =>
                        revokeShare.mutateAsync({ id: share.id, pageId })
                      }
                    />
                  </li>
                ))}
              </ul>
            )}
          </section>

          <CreateShareForm
            pending={createShare.isPending}
            error={createShare.error}
            onCreate={(input) => createShare.mutateAsync(input)}
            onReset={() => createShare.reset()}
          />
        </SheetBody>
      </SheetContent>
    </Sheet>
  )
}
