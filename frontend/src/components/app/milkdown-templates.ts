import { editorViewCtx, parserCtx } from '@milkdown/kit/core'
import { Slice } from '@milkdown/kit/prose/model'
import type { Ctx } from '@milkdown/ctx'

// Built-in templates inserted from the slash menu — the markdown-safe stand-in
// for Notion "buttons". Each template is just a markdown snippet; inserting it
// parses the markdown to PM nodes and drops them at the cursor, so there's
// nothing proprietary and the result is ordinary editable content.
// User-defined templates are deferred (would need storage).

export interface TemplateDef {
  id: string
  label: string
  keywords: string[]
  markdown: string
}

export const TEMPLATES: TemplateDef[] = [
  {
    id: 'tpl-meeting',
    label: 'Meeting notes',
    keywords: ['template', 'meeting', 'notes', 'agenda'],
    markdown: `## Meeting notes

**Date:**
**Attendees:**

### Agenda
-

### Notes
-

### Action items
- [ ]
`,
  },
  {
    id: 'tpl-decision',
    label: 'Decision record',
    keywords: ['template', 'decision', 'adr', 'record'],
    markdown: `## Decision

**Status:** Proposed
**Date:**

### Context

### Decision

### Consequences
`,
  },
  {
    id: 'tpl-weekly',
    label: 'Weekly review',
    keywords: ['template', 'weekly', 'review', 'retro'],
    markdown: `## Weekly review

### Done this week
- [ ]

### Next week
- [ ]

### Blockers
-
`,
  },
]

// Parse a markdown snippet and replace the current selection with its blocks.
export function insertTemplate(ctx: Ctx, markdown: string) {
  const view = ctx.get(editorViewCtx)
  const parser = ctx.get(parserCtx)
  const doc = parser(markdown)
  if (!doc) return
  const slice = new Slice(doc.content, 0, 0)
  view.dispatch(view.state.tr.replaceSelection(slice).scrollIntoView())
  view.focus()
}
