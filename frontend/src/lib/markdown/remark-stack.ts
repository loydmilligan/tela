import { unified, type Processor } from 'unified'
import remarkParse from 'remark-parse'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'
import remarkDirective from 'remark-directive'
import type { Root } from 'mdast'
import { calloutsRemark } from './transforms/callouts'
import { highlightRemark } from './transforms/highlight'
import { excalidrawRemark } from './transforms/excalidraw'

// Single parse source for the read-only VIEW renderer (docs/view-edit-split.md).
//
// The custom transforms (`calloutsRemark`, `highlightRemark`) are the EXACT same
// functions the Milkdown editor wraps via `$remark`, so view and edit parse
// markdown identically — no second parser to drift. The stock plugins
// (gfm/math/directive) are the same ones the editor's commonmark+gfm preset and
// `mathRemarkPlugin`/`directiveRemarkPlugin` are built on, so the base grammar
// matches too. Directive blocks (pull-quote, embed, tabs, …) parse into
// `containerDirective`/`leafDirective` mdast nodes here; the view renderer maps
// them by `name` (mirroring each block's schema `parseMarkdown`).
let processor: Processor<Root, Root, Root, Root, string> | null = null

function getProcessor() {
  if (!processor) {
    processor = unified()
      .use(remarkParse)
      .use(remarkGfm)
      .use(remarkMath)
      .use(remarkDirective)
      .use(calloutsRemark)
      .use(highlightRemark as never)
      .use(excalidrawRemark) as unknown as Processor<Root, Root, Root, Root, string>
  }
  return processor
}

// Parse a page body (canonical markdown) into the transformed mdast tree the
// view renderer walks. `parse` applies the parser extensions (gfm/math/
// directive); `runSync` applies the mdast transformers (callouts, highlight).
export function parsePageMarkdown(body: string): Root {
  const p = getProcessor()
  return p.runSync(p.parse(body))
}
