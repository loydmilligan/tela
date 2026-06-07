import { Plugin } from '@milkdown/kit/prose/state'
import { Selection } from '@milkdown/kit/prose/state'
import type { EditorView } from '@milkdown/kit/prose/view'
import { uploadAttachment } from '../../lib/queries/attachments'
import { insertFileNode } from './milkdown-file'

// Drop / paste upload UX. Any file dropped or pasted into the editor is uploaded
// to POST /api/pages/{pageId}/attachments (the unified space_files store) and
// inserted inline: images as a standard `![alt](url)` node, everything else as a
// `:::file` card. The body stays canonical markdown. Wired only in editable,
// non-share, page-known mode (see milkdown-editor.tsx). URL pastes / rich HTML
// fall through to the existing handlers (only file payloads are intercepted).
//
// After each upload a `tela:attachments-changed` event fires so the page's
// AttachmentStrip refetches (the new file is now parented to the page).

function filesFrom(dt: DataTransfer | null): File[] {
  if (!dt) return []
  return Array.from(dt.files)
}

function isImage(file: File): boolean {
  return file.type.startsWith('image/')
}

async function uploadAndInsert(
  view: EditorView,
  pageId: number,
  files: File[],
  pos: number,
) {
  for (const file of files) {
    try {
      const a = await uploadAttachment(pageId, file)
      if (isImage(file)) {
        const imageType = view.state.schema.nodes.image
        if (!imageType) continue
        const alt = file.name.replace(/\.[^.]+$/, '')
        const node = imageType.create({ src: a.url, alt })
        const at = Math.min(pos, view.state.doc.content.size)
        const sel = Selection.near(view.state.doc.resolve(at))
        view.dispatch(
          view.state.tr.setSelection(sel).replaceSelectionWith(node, false).scrollIntoView(),
        )
      } else {
        insertFileNode(view, { url: a.url, name: a.name, size: a.byte_size }, pos)
      }
      window.dispatchEvent(
        new CustomEvent('tela:attachments-changed', { detail: { pageId } }),
      )
    } catch {
      // Best-effort: a failed upload just doesn't insert. The user can retry.
    }
  }
}

export function createAttachmentDropPlugin(pageId: number): Plugin {
  return new Plugin({
    props: {
      handlePaste: (view, event) => {
        const files = filesFrom(event.clipboardData)
        if (files.length === 0) return false
        event.preventDefault()
        void uploadAndInsert(view, pageId, files, view.state.selection.from)
        return true
      },
      handleDOMEvents: {
        drop: (view, event) => {
          const files = filesFrom(event.dataTransfer)
          if (files.length === 0) return false
          event.preventDefault()
          const dropped = view.posAtCoords({
            left: event.clientX,
            top: event.clientY,
          })
          const pos = dropped?.pos ?? view.state.selection.from
          void uploadAndInsert(view, pageId, files, pos)
          return true
        },
      },
    },
  })
}
