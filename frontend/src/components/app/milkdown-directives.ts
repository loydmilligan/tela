import { $remark } from '@milkdown/kit/utils'
import remarkDirective from 'remark-directive'

// Shared remark-directive plugin: parses `:::name` container directives (and
// serializes them back via mdast-util-directive). The `:::name` family is the
// container-block convention for tela's structured blocks (tabs, kanban) — a
// text-expressible extension, same spirit as the GFM-alert callouts. Consumer
// schemas (tabs, kanban) match `containerDirective` by name; an unrecognized
// directive falls through to its default rendering (plain nested content), so
// adding this is safe even before every consumer exists.
export const directiveRemarkPlugin = $remark(
  'telaDirectives',
  () => remarkDirective as never,
)
