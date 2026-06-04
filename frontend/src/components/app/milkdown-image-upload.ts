import { Plugin } from '@milkdown/kit/prose/state'
import { Selection } from '@milkdown/kit/prose/state'
import type { EditorView } from '@milkdown/kit/prose/view'

// Image-upload UX. Pasting or dropping an image file uploads it to
// POST /api/pages/{pageId}/images (the content-addressed BLOB sidecar) and
// inserts a standard `![alt](url)` image node — so the page body stays
// canonical markdown; only the affordance was missing. Wired only in editable,
// non-share, page-known mode (see milkdown-editor.tsx), mirroring the mira
// paste-hook. URL pastes / rich HTML fall through to the existing handlers.

function imageFilesFrom(dt: DataTransfer | null): File[] {
  if (!dt) return []
  const out: File[] = []
  for (const item of Array.from(dt.files)) {
    if (item.type.startsWith('image/')) out.push(item)
  }
  return out
}

async function uploadAndInsert(
  view: EditorView,
  pageId: number,
  files: File[],
  pos: number,
) {
  for (const file of files) {
    try {
      const form = new FormData()
      form.append('file', file)
      const res = await fetch(`/api/pages/${pageId}/images`, {
        method: 'POST',
        body: form,
        credentials: 'include',
      })
      if (!res.ok) continue
      const { url } = (await res.json()) as { url?: string }
      if (!url) continue
      const imageType = view.state.schema.nodes.image
      if (!imageType) continue
      const alt = file.name.replace(/\.[^.]+$/, '')
      const node = imageType.create({ src: url, alt })
      // Insert at the recorded position (paste = cursor, drop = drop point),
      // clamped + snapped to the nearest valid selection so an image lands in
      // a textblock regardless of where the drop hit.
      const at = Math.min(pos, view.state.doc.content.size)
      const sel = Selection.near(view.state.doc.resolve(at))
      view.dispatch(
        view.state.tr.setSelection(sel).replaceSelectionWith(node, false).scrollIntoView(),
      )
    } catch {
      // Best-effort: a failed upload just doesn't insert. Minimal handling per
      // project convention; the user can retry.
    }
  }
}

export function createImageUploadPlugin(pageId: number): Plugin {
  return new Plugin({
    props: {
      handlePaste: (view, event) => {
        const files = imageFilesFrom(event.clipboardData)
        if (files.length === 0) return false
        event.preventDefault()
        void uploadAndInsert(view, pageId, files, view.state.selection.from)
        return true
      },
      handleDOMEvents: {
        drop: (view, event) => {
          const files = imageFilesFrom(event.dataTransfer)
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
