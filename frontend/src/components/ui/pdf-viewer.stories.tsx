import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Button } from './button'
import { PdfPreviewDialog } from './pdf-viewer'

// A self-contained, valid one-page PDF as a data URL, built at module load so
// the story needs no network and no fixture file. pdf.js renders it exactly as
// it would a real /api/files/… serve URL.
function tinyPdfDataUrl(): string {
  const objects: string[] = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 360 220] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>',
    '', // content stream — filled in below
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
  ]
  const stream =
    'BT /F1 22 Tf 40 130 Td (Hello, tela PDF preview) Tj ' +
    '0 -36 Td /F1 13 Tf (react-pdf + pdf.js, rendered client-side) Tj ET'
  objects[3] = `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`

  let body = '%PDF-1.4\n'
  const offsets: number[] = []
  objects.forEach((obj, i) => {
    offsets.push(body.length)
    body += `${i + 1} 0 obj\n${obj}\nendobj\n`
  })
  const xrefStart = body.length
  let xref = `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`
  offsets.forEach((off) => {
    xref += `${String(off).padStart(10, '0')} 00000 n \n`
  })
  body += `${xref}trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xrefStart}\n%%EOF`
  return `data:application/pdf;base64,${btoa(body)}`
}

const SAMPLE = tinyPdfDataUrl()

function Demo({ startOpen = false }: { startOpen?: boolean }) {
  const [open, setOpen] = useState(startOpen)
  return (
    <>
      <Button variant="secondary" size="sm" onClick={() => setOpen(true)}>
        Preview PDF
      </Button>
      <PdfPreviewDialog
        url={SAMPLE}
        name="sample.pdf"
        open={open}
        onOpenChange={setOpen}
      />
    </>
  )
}

const meta: Meta<typeof PdfPreviewDialog> = {
  title: 'UI/PdfViewer',
  component: PdfPreviewDialog,
}
export default meta

type Story = StoryObj<typeof PdfPreviewDialog>

// Closed by default — click the button to open the viewer.
export const Default: Story = { render: () => <Demo /> }

// Rendered already open, showing the sample document.
export const Open: Story = { render: () => <Demo startOpen /> }
