import { useCallback, useMemo, useState } from 'react'
import { Download } from 'lucide-react'
import { modelToCsv, parse } from '@defterjs/core'
import { createEngine } from '@defterjs/formula'
import { Button } from '../ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'

// Export affordance for a sheet: CSV of the active sheet, or the whole workbook
// as XLSX. Both are generated client-side from the canonical Defter body —
// formulas are materialized to their computed values via the (dependency-free)
// engine, so exports carry numbers, not `=source`. exceljs rides in only when
// XLSX is actually chosen (dynamic import in @defterjs/xlsx).

function downloadBlob(data: BlobPart, name: string, type: string): void {
  const url = URL.createObjectURL(new Blob([data], { type }))
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

function fileBase(s: string): string {
  return (
    s
      .trim()
      .replace(/[^\w.-]+/g, '-')
      .replace(/^-+|-+$/g, '') || 'sheet'
  )
}

export function SheetExportMenu({
  body,
  title,
  activeSheet,
}: {
  body: string
  title: string
  activeSheet: number
}) {
  const engine = useMemo(() => createEngine(), [])
  const [busy, setBusy] = useState(false)

  const exportCsv = useCallback(() => {
    const model = parse(body)
    const idx = Math.min(activeSheet, model.sheets.length - 1)
    const csv = modelToCsv(model, {
      sheetIndex: idx,
      computed: engine.compute(model),
    })
    const sheetName = model.sheets[idx]?.name || title
    downloadBlob(csv, `${fileBase(sheetName)}.csv`, 'text/csv;charset=utf-8')
  }, [body, activeSheet, engine, title])

  const exportXlsx = useCallback(async () => {
    if (busy) return
    setBusy(true)
    try {
      const model = parse(body)
      const mod = await import('@defterjs/xlsx')
      const buf = await mod.exportXlsx(model, { computed: engine.compute(model) })
      downloadBlob(
        buf,
        `${fileBase(title)}.xlsx`,
        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
      )
    } finally {
      setBusy(false)
    }
  }, [body, engine, title, busy])

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" aria-label="Export sheet" disabled={busy}>
          <Download width={16} height={16} aria-hidden />
          <span className="hidden sm:inline">Export</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={exportCsv}>
          This sheet (.csv)
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => void exportXlsx()}>
          Workbook (.xlsx)
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
