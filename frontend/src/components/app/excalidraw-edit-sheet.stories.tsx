import { useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '../ui/sheet'

// Renders the SAME Sheet chrome as ExcalidrawEditSheet without lazy-loading
// the @excalidraw/excalidraw runtime. The real Sheet imports the library on
// open (~290 KB gz); the stories don't need to validate that path — they
// only validate header + body slot + alt-text input + Save/Cancel footer
// across the four runtime states.
//
// Keep the chrome shape in sync with excalidraw-edit-sheet.tsx if it
// changes — the production component is the source of truth; this preview
// mirrors it for storybook review only.

interface ShellProps {
  bodyContent: React.ReactNode
  altText: string
  setAltText: (next: string) => void
  status: 'idle' | 'saving' | 'error'
  errorMessage: string | null
}

function ExcalidrawEditSheetShell({
  bodyContent,
  altText,
  setAltText,
  status,
  errorMessage,
}: ShellProps) {
  const [open, setOpen] = useState(true)
  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetContent
        side="right"
        className="!w-screen sm:!max-w-none flex flex-col"
        onOpenAutoFocus={(e) => e.preventDefault()}
      >
        <SheetHeader>
          <SheetTitle>Edit diagram</SheetTitle>
          <SheetDescription>
            Draw your diagram, optionally add alt text, then Save to embed it
            in the page.
          </SheetDescription>
        </SheetHeader>

        <SheetBody className="p-0 min-h-0 flex-1">{bodyContent}</SheetBody>

        <SheetFooter className="flex-col items-stretch gap-[var(--space-2)] sm:flex-row sm:items-center">
          <Input
            value={altText}
            onChange={(e) => setAltText(e.target.value)}
            placeholder="Add alt text? (Helps screen readers and broken-image fallback)"
            aria-label="Diagram alt text"
            className="flex-1"
          />
          {status === 'error' && errorMessage ? (
            <p
              role="alert"
              className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
            >
              {errorMessage}
            </p>
          ) : null}
          <div className="flex items-center justify-end gap-[var(--space-2)]">
            <Button type="button" variant="ghost" disabled={status === 'saving'}>
              Cancel
            </Button>
            <Button type="button" variant="primary" disabled={status === 'saving'}>
              {status === 'saving' ? 'Saving…' : 'Save'}
            </Button>
          </div>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

function StubCanvas({ label }: { label: string }) {
  return (
    <div
      className="h-full w-full flex items-center justify-center text-[length:var(--text-base)] text-[var(--text-muted)] font-[family-name:var(--font-sans)] bg-[var(--surface-2)]"
      aria-label="Excalidraw mock canvas"
    >
      {label}
    </div>
  )
}

const meta: Meta = {
  title: 'App/ExcalidrawEditSheet',
  parameters: { layout: 'fullscreen' },
}
export default meta

type Story = StoryObj

export const FreshInsert: Story = {
  name: 'Empty (fresh slash-menu insert)',
  render: () => {
    const [altText, setAltText] = useState('')
    return (
      <ExcalidrawEditSheetShell
        bodyContent={<StubCanvas label="Excalidraw mock — empty canvas" />}
        altText={altText}
        setAltText={setAltText}
        status="idle"
        errorMessage={null}
      />
    )
  },
}

export const Populated: Story = {
  name: 'Populated (editing an existing diagram)',
  render: () => {
    const [altText, setAltText] = useState('Flowchart of the auth pipeline')
    return (
      <ExcalidrawEditSheetShell
        bodyContent={
          <StubCanvas label="Excalidraw mock — populated with example diagram" />
        }
        altText={altText}
        setAltText={setAltText}
        status="idle"
        errorMessage={null}
      />
    )
  },
}

export const Saving: Story = {
  name: 'Saving (PUT in flight)',
  render: () => {
    const [altText, setAltText] = useState('Wireframe v3')
    return (
      <ExcalidrawEditSheetShell
        bodyContent={<StubCanvas label="Excalidraw mock — uploading…" />}
        altText={altText}
        setAltText={setAltText}
        status="saving"
        errorMessage={null}
      />
    )
  },
}

export const Error: Story = {
  name: 'Error (PUT failed — Sheet stays open)',
  render: () => {
    const [altText, setAltText] = useState('Network outage diagram')
    return (
      <ExcalidrawEditSheetShell
        bodyContent={<StubCanvas label="Excalidraw mock — try again" />}
        altText={altText}
        setAltText={setAltText}
        status="error"
        errorMessage="Diagram too large — try simplifying it."
      />
    )
  },
}
