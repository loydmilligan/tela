// Convert an uploaded CSV or XLSX file into a Defter markdown body (+ a suggested
// title from the filename), for creating a sheet page from an existing
// spreadsheet. @defterjs is imported dynamically so it (and exceljs for XLSX)
// only load when a file is actually imported — never in the main bundle.

export async function fileToSheetBody(
  file: File,
): Promise<{ body: string; name: string }> {
  const name = file.name.replace(/\.(csv|xlsx|xls)$/i, '').trim()
  const { serialize } = await import('@defterjs/core')
  if (/\.xlsx?$/i.test(file.name)) {
    const buf = await file.arrayBuffer()
    const { importXlsx } = await import('@defterjs/xlsx')
    return { body: serialize(await importXlsx(buf)), name }
  }
  // Default to CSV for anything else (incl. text/csv, .txt exports).
  const { csvToModel } = await import('@defterjs/core')
  const text = await file.text()
  return { body: serialize(csvToModel(text, name || 'Sheet1')), name }
}
