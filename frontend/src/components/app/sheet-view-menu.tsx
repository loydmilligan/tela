import { useCallback } from 'react'
import { Check, Snowflake } from 'lucide-react'
import { Button } from '../ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'
import { useUpdatePage } from '../../lib/queries/pages'

// Per-sheet view options — currently freeze header row / first column. Freezing
// is opt-in (a sheet may have no header), persisted as page props so it sticks
// across sessions + peers. (Encoding it in the defter-style block so it travels
// with an exported/synced document is a defter-side follow-up.)
export function SheetViewMenu({
  pageId,
  props,
}: {
  pageId: number
  props?: Record<string, unknown>
}) {
  const updatePage = useUpdatePage()
  const freezeHeader = props?.sheetFreezeHeader === true
  const freezeCol = props?.sheetFreezeCol === true

  const toggle = useCallback(
    (key: 'sheetFreezeHeader' | 'sheetFreezeCol', cur: boolean) => {
      void updatePage.mutateAsync({
        id: pageId,
        props: { ...(props ?? {}), [key]: !cur },
      })
    },
    [props, pageId, updatePage],
  )

  const item = (label: string, on: boolean, onSelect: () => void) => (
    <DropdownMenuItem
      onSelect={(e) => {
        e.preventDefault() // keep the menu open so both can be toggled
        onSelect()
      }}
    >
      {on ? (
        <Check width={14} height={14} />
      ) : (
        <span className="inline-block w-[14px]" />
      )}
      {label}
    </DropdownMenuItem>
  )

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" aria-label="Freeze options">
          <Snowflake width={16} height={16} aria-hidden />
          <span className="hidden sm:inline">Freeze</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {item('Freeze header row', freezeHeader, () =>
          toggle('sheetFreezeHeader', freezeHeader),
        )}
        {item('Freeze first column', freezeCol, () =>
          toggle('sheetFreezeCol', freezeCol),
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
