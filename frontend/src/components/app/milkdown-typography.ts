import { $prose } from '@milkdown/kit/utils'
import {
  ellipsis,
  emDash,
  inputRules,
  smartQuotes,
} from '@milkdown/kit/prose/inputrules'

// Smart-typography input rules (ProseMirror built-ins): straight quotes → curly
// “ ” ‘ ’, `--` → em-dash —, `...` → ellipsis …. They fire as you type and are
// skipped inside code blocks (the inputRules plugin ignores textblocks whose
// node spec is `code`), so fenced/indented code and `inline code` stay literal.
// The resulting characters round-trip verbatim through markdown, so pages.body
// stays canonical — no non-markdown state is introduced.
export const typographyInputRules = $prose(() =>
  inputRules({ rules: [...smartQuotes, ellipsis, emDash] }),
)
