import { $ctx, $inputRule } from '@milkdown/kit/utils'
import { InputRule } from '@milkdown/kit/prose/inputrules'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'
import { ensureEmojiLoaded, lookupEmoji } from '../../lib/emoji'

// `:rocket:` → 🚀, fired on the closing colon. tela keeps the actual Unicode
// emoji in the canonical markdown body (not the shortcode), so this is a pure
// text replacement — no schema node, no serialization, nothing for the reader
// or backend to know about. Mirrors the `$math$` input rule (milkdown-math.ts).
//
// The shortcode set is GitHub's (gemoji); the dataset is lazy-loaded, so we
// kick the import off here at module load. By the time anyone types a full
// `:shortcode:` it's resolved; on the vanishingly rare pre-load keystroke
// `lookupEmoji` returns undefined and the text is simply left as-is (retyping
// the closing colon re-fires the rule).
void ensureEmojiLoaded()

// `:` + a shortcode body (letters, digits, `_ + -`) + closing `:`. The body
// must start with an alphanumeric so a bare `::` or a `:-)` emoticon doesn't
// trigger, and we anchor the match at the cursor (`$`).
const SHORTCODE_RE = /:([a-z0-9][a-z0-9_+-]*):$/

export const emojiInputRule = $inputRule(
  () =>
    new InputRule(SHORTCODE_RE, (state, match, start, end) => {
      const emoji = lookupEmoji(match[1])
      if (!emoji) return null
      return state.tr.insertText(emoji, start, end)
    }),
)

// ── `/` emoji picker (phase 2) ───────────────────────────────────────────────
// The slash menu's "Emoji" item opens a visual grid picker. Same trampoline
// shape as the excalidraw Edit Sheet: a ctx slice holds an open-handler the
// host (MilkdownEditor) sets to a React state setter; the slash `run` captures
// the caret coords + position and fires it; the picker inserts the chosen char
// back at that position. Null handler (share / read-only) ⇒ the run no-ops.

export interface EmojiPickerRequest {
  // Screen-space caret coords captured at open time, to anchor the popover.
  anchor: { left: number; top: number; bottom: number }
  // ProseMirror position to insert the picked emoji at (the caret, after the
  // `/emoji` trigger text has been cleared by the slash menu).
  pos: number
}
export type EmojiPickerOpenHandler = (req: EmojiPickerRequest) => void

export const emojiPickerOpenCtx = $ctx<EmojiPickerOpenHandler | null, 'emojiPickerOpen'>(
  null,
  'emojiPickerOpen',
)

// Slash-menu `run` for the "Emoji" item. Runs after the menu has cleared the
// `/query` text, so the caret sits where the `/` was — exactly where the
// emoji should land.
export function openEmojiPicker(ctx: Ctx) {
  const handler = ctx.get(emojiPickerOpenCtx.key)
  if (!handler) return
  const view = ctx.get(editorViewCtx)
  const { from } = view.state.selection
  const coords = view.coordsAtPos(from)
  handler({
    pos: from,
    anchor: { left: coords.left, top: coords.top, bottom: coords.bottom },
  })
}

// Insert the chosen emoji char at a saved position (the picker popover steals
// focus from the editor, so we can't rely on the live selection).
export function insertEmojiAt(ctx: Ctx, pos: number, emoji: string) {
  const view = ctx.get(editorViewCtx)
  const size = view.state.doc.content.size
  // Guard against a stale position (shouldn't happen — the picker is modal over
  // a static doc — but clamp rather than throw if it ever does).
  const at = Math.min(pos, size)
  view.dispatch(view.state.tr.insertText(emoji, at).scrollIntoView())
  view.focus()
}
