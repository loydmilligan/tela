import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button } from './button'
import { Toaster, toast } from './toast'

// The toast store is module-level, so a story just renders <Toaster/> and fires
// toast() from a trigger. Variants map to semantic tokens (success=green,
// destructive=danger).
const meta: Meta = {
  title: 'UI/Toast',
  render: () => (
    <div style={{ minHeight: 200 }}>
      <div style={{ display: 'flex', gap: 8 }}>
        <Button onClick={() => toast({ title: 'Saved', description: 'Your changes were saved.' })}>
          Default
        </Button>
        <Button
          variant="secondary"
          onClick={() => toast({ variant: 'success', title: 'Uploaded', description: 'image.png added.' })}
        >
          Success
        </Button>
        <Button
          variant="danger"
          onClick={() =>
            toast({ variant: 'destructive', title: 'Upload failed', description: 'Could not upload image.png — try again.' })
          }
        >
          Error
        </Button>
      </div>
      <Toaster />
    </div>
  ),
}
export default meta

type Story = StoryObj

export const Playground: Story = {}
