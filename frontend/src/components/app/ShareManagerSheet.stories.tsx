import { useMemo, useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Button } from '../ui/button'
import { ShareManagerSheet } from './ShareManagerSheet'
import { type ShareDTO, shareKeys } from '../../lib/queries/share'

const meta: Meta = {
  title: 'App/ShareManagerSheet',
  parameters: { layout: 'fullscreen' },
}
export default meta

type Story = StoryObj

// Build a ShareDTO fixture. Timestamps are SQLite-native (UTC, no zone marker)
// so they parse through `parseSqliteTs` just like real data.
function mkShare(partial: Partial<ShareDTO> & { id: number; token: string }): ShareDTO {
  return {
    page_id: 42,
    include_descendants: false,
    has_password: false,
    created_by: 2,
    created_at: '2026-05-20 12:00:00',
    expires_at: null,
    revoked_at: null,
    url: `https://tela.cagdas.io/share/${partial.token}`,
    ...partial,
  }
}

// Future/past timestamps for the expiry-state row. Returned in the SQLite
// wire format the FE parses.
function inHours(h: number): string {
  const d = new Date(Date.now() + h * 3_600_000)
  return d.toISOString().replace('T', ' ').slice(0, 19)
}

function makeQueryClient(seed: ShareDTO[]): QueryClient {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  qc.setQueryData(shareKeys.list(42), seed)
  return qc
}

interface ProvidersProps {
  children: React.ReactNode
  seed: ShareDTO[]
}

function Providers({ children, seed }: ProvidersProps) {
  const qc = useMemo(() => makeQueryClient(seed), [seed])
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
}

interface SheetHostProps {
  seed: ShareDTO[]
}

function SheetHost({ seed }: SheetHostProps) {
  const [open, setOpen] = useState(true)
  return (
    <Providers seed={seed}>
      <div className="min-h-[80vh] p-[var(--space-7)]">
        <Button onClick={() => setOpen(true)}>Open share manager</Button>
        <ShareManagerSheet pageId={42} open={open} onOpenChange={setOpen} />
      </div>
    </Providers>
  )
}

export const Empty: Story = {
  name: 'Empty list + create form',
  render: () => <SheetHost seed={[]} />,
}

export const SingleShare: Story = {
  name: 'One share (no descendants, no password)',
  render: () => (
    <SheetHost
      seed={[
        mkShare({
          id: 1,
          token: 'aaaa1111bbbb2222cccc3333dddd4444',
          created_at: '2026-05-20 10:00:00',
        }),
      ]}
    />
  ),
}

export const Mixed: Story = {
  name: 'Three shares — password, expiring soon, expired',
  render: () => (
    <SheetHost
      seed={[
        mkShare({
          id: 3,
          token: 'eeee5555ffff6666',
          include_descendants: true,
          has_password: true,
          expires_at: inHours(72),
          created_at: '2026-05-20 09:30:00',
        }),
        mkShare({
          id: 2,
          token: 'cccc3333dddd4444',
          has_password: true,
          expires_at: inHours(-3),
          created_at: '2026-05-18 14:00:00',
        }),
        mkShare({
          id: 1,
          token: 'aaaa1111bbbb2222',
          created_at: '2026-05-15 16:20:00',
        }),
      ]}
    />
  ),
}

// Storybook can't easily replay a 400 from the create mutation without
// patching fetch, so this variant shows the create-form's inline-error
// channel by pre-seeding a manual error via a thin wrapper. Approximates
// the visual of "Expiry must be in the future." landing under the form.
function CreateFormErrorHost() {
  const [open, setOpen] = useState(true)
  const qc = useMemo(() => {
    const c = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    c.setQueryData(shareKeys.list(42), [])
    // Pre-populate the mutation cache with an error state so the create
    // form renders its inline alert. Mutation observers don't read from
    // qc.setQueryData; the simplest path is to leave this story as a
    // pure visual placeholder — devs verifying the error visual can
    // submit a past-dated expiry against a real backend instead.
    return c
  }, [])
  return (
    <QueryClientProvider client={qc}>
      <div className="min-h-[80vh] p-[var(--space-7)]">
        <Button onClick={() => setOpen(true)}>Open share manager</Button>
        <ShareManagerSheet pageId={42} open={open} onOpenChange={setOpen} />
        <p className="mt-[var(--space-4)] text-[length:var(--text-xs)] text-[var(--text-muted)]">
          Enter a past expiry (e.g. <code>2020-01-01 00:00:00</code>) and submit
          against a real backend to see the inline "Expiry must be in the
          future." error.
        </p>
      </div>
    </QueryClientProvider>
  )
}

export const CreateFormError: Story = {
  name: 'Create form — past-dated expiry hint',
  render: () => <CreateFormErrorHost />,
}
