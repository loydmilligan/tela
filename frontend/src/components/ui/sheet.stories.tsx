import type { Meta, StoryObj } from '@storybook/react-vite'
import {
  Sheet,
  SheetBody,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from './sheet'
import { Button } from './button'
import { TextArea } from './textarea'

const meta: Meta<typeof Sheet> = {
  title: 'UI/Sheet',
  component: Sheet,
}
export default meta

type Story = StoryObj<typeof Sheet>

export const SlideFromRight: Story = {
  render: () => (
    <Sheet>
      <SheetTrigger asChild>
        <Button>Open sheet (right)</Button>
      </SheetTrigger>
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>Comments</SheetTitle>
          <SheetDescription>
            Slide-from-right panel. Backdrop click, ESC, and the close button
            all dismiss.
          </SheetDescription>
        </SheetHeader>
        <SheetBody>
          <p
            className="text-[length:var(--text-sm)]"
            style={{ color: 'var(--text-primary)' }}
          >
            Body content scrolls independently of the header and footer.
          </p>
        </SheetBody>
        <SheetFooter>
          <SheetClose asChild>
            <Button variant="ghost">Close</Button>
          </SheetClose>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  ),
}

export const SlideFromLeft: Story = {
  render: () => (
    <Sheet>
      <SheetTrigger asChild>
        <Button>Open sheet (left)</Button>
      </SheetTrigger>
      <SheetContent side="left">
        <SheetHeader>
          <SheetTitle>Navigation</SheetTitle>
          <SheetDescription>
            Mirrored variant — slides in from the left edge.
          </SheetDescription>
        </SheetHeader>
        <SheetBody>
          <p
            className="text-[length:var(--text-sm)]"
            style={{ color: 'var(--text-primary)' }}
          >
            Any sheet-shaped layout works on either side.
          </p>
        </SheetBody>
      </SheetContent>
    </Sheet>
  ),
}

function FakeComment({
  author,
  body,
  time,
}: {
  author: string
  body: string
  time: string
}) {
  return (
    <div
      className="flex flex-col gap-[var(--space-1)] py-[var(--space-3)]"
      style={{ borderBottom: '1px solid var(--border-subtle)' }}
    >
      <div className="flex items-baseline gap-[var(--space-2)]">
        <span
          className="text-[length:var(--text-sm)]"
          style={{ color: 'var(--text-primary)', fontWeight: 600 }}
        >
          {author}
        </span>
        <span
          className="text-[length:var(--text-xs)]"
          style={{ color: 'var(--text-muted)' }}
        >
          {time}
        </span>
      </div>
      <p
        className="m-0 text-[length:var(--text-sm)]"
        style={{ color: 'var(--text-primary)', lineHeight: 'var(--leading-normal)' }}
      >
        {body}
      </p>
    </div>
  )
}

export const NestedInPageView: Story = {
  render: () => (
    <div
      style={{
        display: 'flex',
        gap: 'var(--space-4)',
        minHeight: '420px',
      }}
    >
      <div
        style={{
          flex: 1,
          padding: 'var(--space-6)',
          background: 'var(--surface-1)',
          border: '1px solid var(--border-subtle)',
          borderRadius: 'var(--radius-md)',
        }}
      >
        <h2
          style={{
            margin: '0 0 var(--space-3)',
            fontSize: 'var(--text-2xl)',
            color: 'var(--text-primary)',
          }}
        >
          Quarterly review
        </h2>
        <p style={{ color: 'var(--text-primary)', marginBottom: 'var(--space-4)' }}>
          This shipped three releases on schedule. The retention chart shows a
          17% bump after the inbox redesign — worth calling out in the next
          all-hands deck.
        </p>
        <Sheet>
          <SheetTrigger asChild>
            <Button variant="ghost">Comments (3)</Button>
          </SheetTrigger>
          <SheetContent side="right">
            <SheetHeader>
              <SheetTitle>Comments</SheetTitle>
              <SheetDescription>3 threads on this page</SheetDescription>
            </SheetHeader>
            <SheetBody>
              <div
                style={{
                  marginBottom: 'var(--space-4)',
                  padding: 'var(--space-3)',
                  borderRadius: 'var(--radius-md)',
                  border: '1px solid var(--border-subtle)',
                  background: 'var(--surface-2)',
                }}
              >
                <TextArea
                  placeholder="Select text in the editor to comment on a passage."
                  rows={3}
                />
                <div
                  style={{
                    display: 'flex',
                    justifyContent: 'flex-end',
                    marginTop: 'var(--space-2)',
                  }}
                >
                  <Button size="sm" disabled>
                    Comment
                  </Button>
                </div>
              </div>
              <FakeComment
                author="admin"
                time="2 min ago"
                body="Worth flagging the retention bump in next week's all-hands."
              />
              <FakeComment
                author="carol"
                time="yesterday"
                body="+1 — also we should re-run the cohort against the warm-theme launch."
              />
              <FakeComment
                author="bob"
                time="3 days ago"
                body="Resolved last quarter — keeping here for context."
              />
            </SheetBody>
          </SheetContent>
        </Sheet>
      </div>
    </div>
  ),
}
