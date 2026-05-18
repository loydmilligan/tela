import type { Meta, StoryObj } from '@storybook/react-vite'
import { MoreHorizontal, Pencil, Trash2, FolderPlus, MoveRight } from 'lucide-react'
import { Button } from './button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from './dropdown-menu'

const meta: Meta<typeof DropdownMenu> = {
  title: 'UI/DropdownMenu',
  component: DropdownMenu,
}
export default meta

type Story = StoryObj<typeof DropdownMenu>

export const Basic: Story = {
  render: () => (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" aria-label="Open menu">
          <MoreHorizontal width={16} height={16} />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem>
          <Pencil width={14} height={14} /> Rename
        </DropdownMenuItem>
        <DropdownMenuItem>
          <FolderPlus width={14} height={14} /> New child page
        </DropdownMenuItem>
        <DropdownMenuItem>
          <MoveRight width={14} height={14} /> Move…
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem destructive>
          <Trash2 width={14} height={14} /> Delete
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  ),
}

export const WithLabel: Story = {
  render: () => (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="secondary">Page actions</Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuLabel>Engineering › API design</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem>Rename</DropdownMenuItem>
        <DropdownMenuItem>Move…</DropdownMenuItem>
        <DropdownMenuItem disabled>Duplicate (coming soon)</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem destructive>Delete</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  ),
}
