import { $remark } from '@milkdown/kit/utils'
import remarkDirective from 'remark-directive'
import { unknownDirectivesRemark } from '../../lib/markdown/transforms/unknown-directives'

// Shared remark-directive plugin: parses `:::name` container directives (and
// serializes them back via mdast-util-directive). The `:::name` family is the
// container-block convention for tela's structured blocks (tabs, kanban) — a
// text-expressible extension, same spirit as the GFM-alert callouts. Consumer
// schemas (tabs, kanban) match `containerDirective` by name; an unrecognized
// directive is unwrapped to its nested content by the fallback below before it
// can reach Milkdown's strict parser.
export const directiveRemarkPlugin = $remark(
  'telaDirectives',
  () => remarkDirective as never,
)

// Fallback transformer (runs AFTER remark-directive's parse): unwraps any
// directive WITHOUT a dedicated editor schema into its plain nested content,
// so a foreign/typo'd `:::name` can't reach Milkdown's strict mdast→PM parser
// and hard-crash the whole editor mount (parserMatchError → blank, uneditable
// body). Mirrors the read view's unknown-directive default branch. See
// lib/markdown/transforms/unknown-directives.ts.
export const unknownDirectiveFallbackPlugin = $remark(
  'telaUnknownDirectiveFallback',
  () => unknownDirectivesRemark,
)
