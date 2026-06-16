import { Plugin, PluginKey } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { EditorView } from '@milkdown/kit/prose/view'

// Upload placeholder. While an attachment uploads, we show an inline
// "Uploading <name>…" widget at the insert point instead of awaiting silently,
// then swap it for the real image/file node on success (or just remove it on
// failure — the upload path raises a toast). Classic ProseMirror pattern: a
// DecorationSet of widget decorations keyed by a unique id, mapped through doc
// changes so the placeholder follows edits made while the upload is in flight.

interface AddAction {
  add: { id: object; pos: number; name: string }
}
interface RemoveAction {
  remove: { id: object }
}

const key = new PluginKey<DecorationSet>('tela-upload-placeholder')

function placeholderWidget(name: string): HTMLElement {
  const el = document.createElement('span')
  el.className = 'tela-upload-placeholder'
  el.textContent = `Uploading ${name}…`
  return el
}

export const uploadPlaceholderPlugin = new Plugin<DecorationSet>({
  key,
  state: {
    init: () => DecorationSet.empty,
    apply(tr, set) {
      set = set.map(tr.mapping, tr.doc)
      const action = tr.getMeta(key) as AddAction | RemoveAction | undefined
      if (action && 'add' in action) {
        const deco = Decoration.widget(action.add.pos, placeholderWidget(action.add.name), {
          id: action.add.id,
        })
        set = set.add(tr.doc, [deco])
      } else if (action && 'remove' in action) {
        set = set.remove(set.find(undefined, undefined, (spec) => spec.id === action.remove.id))
      }
      return set
    },
  },
  props: {
    decorations(state) {
      return key.getState(state)
    },
  },
})

export function addUploadPlaceholder(view: EditorView, id: object, pos: number, name: string) {
  view.dispatch(view.state.tr.setMeta(key, { add: { id, pos, name } }))
}

export function removeUploadPlaceholder(view: EditorView, id: object) {
  view.dispatch(view.state.tr.setMeta(key, { remove: { id } }))
}

// Current mapped position of a placeholder, or null if it's gone (e.g. the user
// deleted around it while the upload ran).
export function findUploadPlaceholder(view: EditorView, id: object): number | null {
  const set = key.getState(view.state)
  if (!set) return null
  const found = set.find(undefined, undefined, (spec) => spec.id === id)
  return found.length ? found[0].from : null
}
