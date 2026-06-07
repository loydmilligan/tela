import type { Meta, StoryObj } from '@storybook/react-vite'
import { AttachmentStripView } from './AttachmentStrip'
import type { Attachment } from '../../lib/queries/attachments'

// The presentational strip — fed fixture attachments so the story needs no
// query client or backend. Mirrors what the data container (AttachmentStrip)
// renders once useAttachments resolves.

const meta: Meta<typeof AttachmentStripView> = {
  title: 'App/AttachmentStrip',
  component: AttachmentStripView,
}
export default meta
type Story = StoryObj<typeof AttachmentStripView>

function file(
  id: number,
  name: string,
  mime: string,
  byte_size: number,
  embedded = false,
): Attachment {
  return { id, name, mime, byte_size, hash: `${id}`.repeat(8), url: '#', embedded }
}

const mixed: Attachment[] = [
  file(1, 'q3-planning.pdf', 'application/pdf', 248_000),
  file(2, 'architecture.png', 'image/png', 91_000, true),
  file(3, 'budget.xlsx', 'application/vnd.ms-excel', 18_400),
  file(4, 'notes.txt', 'text/plain', 1_200),
]

export const ReadOnly: Story = {
  args: { attachments: mixed },
}

export const Editable: Story = {
  args: { attachments: mixed, editable: true },
}

export const ManyCollapsed: Story = {
  args: {
    attachments: Array.from({ length: 14 }, (_, i) =>
      file(i + 1, `document-${i + 1}.pdf`, 'application/pdf', 10_000 * (i + 1)),
    ),
  },
}

export const Empty: Story = {
  args: { attachments: [] },
}
