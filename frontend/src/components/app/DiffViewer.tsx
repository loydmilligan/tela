import { useMemo, useState } from 'react'
import { diffLines, type Change } from 'diff'
import { ToggleGroup, ToggleGroupItem } from '../ui/toggle'
import { Card, CardBody, CardHeader, CardTitle, CardDescription } from '../ui/card'
import { cn } from '../../lib/utils'

export type DiffMode = 'split' | 'inline'

export interface DiffViewerProps {
  oldBody: string
  newBody: string
  oldLabel?: string
  newLabel?: string
  defaultMode?: DiffMode
}

type RowKind = 'unchanged' | 'added' | 'removed' | 'empty'

interface DiffRow {
  kind: RowKind
  text: string
  // 1-based line numbers in the original ('left') / new ('right') texts.
  // `null` when the row is a placeholder filler or when the line doesn't
  // exist on that side (added rows have no left lineNo; removed have no right).
  leftLineNo: number | null
  rightLineNo: number | null
}

interface BuiltDiff {
  splitRows: { left: DiffRow; right: DiffRow }[]
  inlineRows: DiffRow[]
  hasChanges: boolean
}

const EMPTY_ROW: DiffRow = {
  kind: 'empty',
  text: '',
  leftLineNo: null,
  rightLineNo: null,
}

// jsdiff Change.value usually ends with a trailing '\n'. Splitting on '\n'
// then leaves an empty trailing element we don't want as a row. If the
// final chunk of the source has no trailing newline, the last entry is the
// last real line and we keep it.
function splitToLines(value: string): string[] {
  if (value === '') return []
  const lines = value.split('\n')
  if (lines[lines.length - 1] === '') lines.pop()
  return lines
}

function buildDiff(oldBody: string, newBody: string): BuiltDiff {
  const parts: Change[] = diffLines(oldBody, newBody, { newlineIsToken: false })
  let leftLine = 1
  let rightLine = 1
  const splitRows: { left: DiffRow; right: DiffRow }[] = []
  const inlineRows: DiffRow[] = []
  let hasChanges = false

  for (const part of parts) {
    const lines = splitToLines(part.value)
    if (part.added) {
      hasChanges = true
      for (const text of lines) {
        const row: DiffRow = {
          kind: 'added',
          text,
          leftLineNo: null,
          rightLineNo: rightLine,
        }
        splitRows.push({ left: EMPTY_ROW, right: row })
        inlineRows.push(row)
        rightLine++
      }
    } else if (part.removed) {
      hasChanges = true
      for (const text of lines) {
        const row: DiffRow = {
          kind: 'removed',
          text,
          leftLineNo: leftLine,
          rightLineNo: null,
        }
        splitRows.push({ left: row, right: EMPTY_ROW })
        inlineRows.push(row)
        leftLine++
      }
    } else {
      for (const text of lines) {
        const row: DiffRow = {
          kind: 'unchanged',
          text,
          leftLineNo: leftLine,
          rightLineNo: rightLine,
        }
        splitRows.push({ left: row, right: row })
        inlineRows.push(row)
        leftLine++
        rightLine++
      }
    }
  }
  return { splitRows, inlineRows, hasChanges }
}

const rowKindClasses: Record<RowKind, string> = {
  unchanged: 'bg-transparent text-[var(--text-primary)]',
  added: 'bg-[var(--diff-add-bg)] text-[var(--diff-add-fg)]',
  removed: 'bg-[var(--diff-del-bg)] text-[var(--diff-del-fg)]',
  empty: 'bg-[var(--surface-2)] text-[var(--text-muted)]',
}

const gutterKindClasses: Record<RowKind, string> = {
  unchanged: 'bg-[var(--diff-gutter-bg)] text-[var(--diff-gutter-fg)]',
  added: 'bg-[var(--diff-add-bg)] text-[var(--diff-add-fg)]',
  removed: 'bg-[var(--diff-del-bg)] text-[var(--diff-del-fg)]',
  empty: 'bg-[var(--diff-gutter-bg)] text-[var(--diff-gutter-fg)]',
}

function inlineMarker(kind: RowKind): string {
  if (kind === 'added') return '+'
  if (kind === 'removed') return '-'
  return ' '
}

export function DiffViewer({
  oldBody,
  newBody,
  oldLabel = 'Old',
  newLabel = 'New',
  defaultMode = 'split',
}: DiffViewerProps) {
  const [mode, setMode] = useState<DiffMode>(defaultMode)
  const built = useMemo(() => buildDiff(oldBody, newBody), [oldBody, newBody])

  if (!built.hasChanges) {
    return (
      <Card className="self-start">
        <CardHeader>
          <CardTitle>No changes</CardTitle>
          <CardDescription>
            No changes between this revision and the current page.
          </CardDescription>
        </CardHeader>
        <CardBody />
      </Card>
    )
  }

  return (
    <div
      className={cn(
        'flex flex-col gap-[var(--space-3)] min-h-0',
        'text-[length:var(--text-sm)] font-[family-name:var(--font-mono)]',
      )}
    >
      <DiffToolbar
        mode={mode}
        onChangeMode={setMode}
        oldLabel={oldLabel}
        newLabel={newLabel}
      />
      <div
        className={cn(
          'border border-[var(--border-subtle)]',
          'rounded-[var(--radius-md)]',
          'bg-[var(--surface-1)]',
          'overflow-auto max-h-[70vh]',
        )}
      >
        {mode === 'split' ? (
          <SplitView rows={built.splitRows} oldLabel={oldLabel} newLabel={newLabel} />
        ) : (
          <InlineView rows={built.inlineRows} />
        )}
      </div>
    </div>
  )
}

interface DiffToolbarProps {
  mode: DiffMode
  onChangeMode: (next: DiffMode) => void
  oldLabel: string
  newLabel: string
}

function DiffToolbar({ mode, onChangeMode, oldLabel, newLabel }: DiffToolbarProps) {
  return (
    <div
      className={cn(
        'flex items-center justify-between gap-[var(--space-3)] flex-wrap',
        'font-[family-name:var(--font-sans)]',
      )}
    >
      <ToggleGroup
        type="single"
        value={mode}
        onValueChange={(next) => {
          if (next === 'split' || next === 'inline') onChangeMode(next)
        }}
        aria-label="Diff layout"
      >
        <ToggleGroupItem value="split" size="sm" aria-label="Side-by-side">
          Split
        </ToggleGroupItem>
        <ToggleGroupItem value="inline" size="sm" aria-label="Inline">
          Inline
        </ToggleGroupItem>
      </ToggleGroup>
      <p className="m-0 text-[length:var(--text-xs)] text-[var(--text-muted)] truncate">
        <span>{oldLabel}</span>
        <span aria-hidden className="mx-[var(--space-2)]">
          →
        </span>
        <span>{newLabel}</span>
      </p>
    </div>
  )
}

interface SplitViewProps {
  rows: { left: DiffRow; right: DiffRow }[]
  oldLabel: string
  newLabel: string
}

function SplitView({ rows, oldLabel, newLabel }: SplitViewProps) {
  return (
    <div
      className="grid grid-cols-2"
      role="table"
      aria-label="Side-by-side diff"
    >
      <PaneHeader label={oldLabel} side="left" />
      <PaneHeader label={newLabel} side="right" />
      {rows.map((row, idx) => (
        <SplitRow
          key={idx}
          left={row.left}
          right={row.right}
          isLast={idx === rows.length - 1}
        />
      ))}
    </div>
  )
}

function PaneHeader({ label, side }: { label: string; side: 'left' | 'right' }) {
  return (
    <div
      className={cn(
        'sticky top-0 z-[1]',
        'bg-[var(--surface-2)] text-[var(--text-muted)]',
        'border-b border-[var(--border-subtle)]',
        side === 'left' && 'border-r',
        'px-[var(--space-3)] py-[var(--space-2)]',
        'text-[length:var(--text-xs)] font-[family-name:var(--font-sans)]',
        'truncate',
      )}
    >
      {label}
    </div>
  )
}

interface SplitRowProps {
  left: DiffRow
  right: DiffRow
  isLast: boolean
}

function SplitRow({ left, right, isLast }: SplitRowProps) {
  return (
    <>
      <DiffCell
        row={left}
        lineNoSide="left"
        borderRight
        borderBottom={!isLast}
      />
      <DiffCell row={right} lineNoSide="right" borderBottom={!isLast} />
    </>
  )
}

interface DiffCellProps {
  row: DiffRow
  lineNoSide: 'left' | 'right'
  borderRight?: boolean
  borderBottom?: boolean
}

function DiffCell({ row, lineNoSide, borderRight, borderBottom }: DiffCellProps) {
  const lineNo = lineNoSide === 'left' ? row.leftLineNo : row.rightLineNo
  return (
    <div
      className={cn(
        'flex items-stretch min-w-0',
        borderRight && 'border-r border-[var(--border-subtle)]',
        borderBottom && 'border-b border-[var(--border-subtle)]',
        rowKindClasses[row.kind],
      )}
      role="row"
    >
      <span
        aria-hidden
        className={cn(
          'shrink-0 select-none text-right',
          'min-w-[var(--space-7)] px-[var(--space-2)]',
          'leading-[var(--leading-relaxed)]',
          'border-r border-[var(--border-subtle)]',
          gutterKindClasses[row.kind],
        )}
      >
        {lineNo ?? ''}
      </span>
      <pre
        className={cn(
          'flex-1 min-w-0 m-0',
          'px-[var(--space-3)]',
          'leading-[var(--leading-relaxed)]',
          'whitespace-pre-wrap break-words',
        )}
      >
        <code>{row.kind === 'empty' ? ' ' : row.text || ' '}</code>
      </pre>
    </div>
  )
}

function InlineView({ rows }: { rows: DiffRow[] }) {
  return (
    <div role="table" aria-label="Inline diff">
      {rows.map((row, idx) => (
        <InlineRow key={idx} row={row} isLast={idx === rows.length - 1} />
      ))}
    </div>
  )
}

function InlineRow({ row, isLast }: { row: DiffRow; isLast: boolean }) {
  return (
    <div
      role="row"
      className={cn(
        'flex items-stretch min-w-0',
        rowKindClasses[row.kind],
        !isLast && 'border-b border-[var(--border-subtle)]',
      )}
    >
      <span
        aria-hidden
        className={cn(
          'shrink-0 select-none text-right',
          'min-w-[var(--space-7)] px-[var(--space-2)]',
          'leading-[var(--leading-relaxed)]',
          'border-r border-[var(--border-subtle)]',
          gutterKindClasses[row.kind],
        )}
      >
        {row.leftLineNo ?? ''}
      </span>
      <span
        aria-hidden
        className={cn(
          'shrink-0 select-none text-right',
          'min-w-[var(--space-7)] px-[var(--space-2)]',
          'leading-[var(--leading-relaxed)]',
          'border-r border-[var(--border-subtle)]',
          gutterKindClasses[row.kind],
        )}
      >
        {row.rightLineNo ?? ''}
      </span>
      <span
        aria-hidden
        className={cn(
          'shrink-0 select-none text-center w-[var(--space-5)]',
          'leading-[var(--leading-relaxed)]',
          gutterKindClasses[row.kind],
          'border-r border-[var(--border-subtle)]',
        )}
      >
        {inlineMarker(row.kind)}
      </span>
      <pre
        className={cn(
          'flex-1 min-w-0 m-0',
          'px-[var(--space-3)]',
          'leading-[var(--leading-relaxed)]',
          'whitespace-pre-wrap break-words',
        )}
      >
        <code>{row.text || ' '}</code>
      </pre>
    </div>
  )
}
