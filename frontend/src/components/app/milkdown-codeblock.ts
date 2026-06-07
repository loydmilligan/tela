import { $prose } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'

// Code-block chrome: a header bar (language label + copy button) wrapped around
// each fenced code block. Implemented as a `code_block` nodeView.
//
// Safe alongside `@milkdown/plugin-prism`: that plugin highlights via an inline
// DecorationSet (NOT a nodeView), so its token spans still land on our
// contentDOM `<code>`. The header is `contenteditable=false` chrome that lives
// in the nodeView's `dom` but outside the content hole, so it never enters the
// document or the markdown round-trip.
//
// Only the `language` attr is surfaced — commonmark drops the fence `meta`
// (info-string tail) on parse/serialize, so a filename label would silently
// lose data on save. That needs a code_block schema override; deferred.

const COPY_LABEL = 'Copy'
const COPIED_LABEL = 'Copied'

export const codeBlockNodeView = $prose(
  () =>
    new Plugin({
      props: {
        nodeViews: {
          code_block: (node) => {
            const lang = (node.attrs.language as string) || ''

            // Content: a standard <pre><code> so the existing prose CSS + prism
            // decorations apply unchanged. <code> is the contentDOM.
            const pre = document.createElement('pre')
            const code = document.createElement('code')
            if (lang) {
              pre.dataset.language = lang
              code.setAttribute('data-language', lang)
            }
            pre.appendChild(code)

            const header = document.createElement('div')
            header.className = 'tela-codeblock-header'
            header.setAttribute('contenteditable', 'false')

            const langLabel = document.createElement('span')
            langLabel.className = 'tela-codeblock-lang'
            langLabel.textContent = lang || 'text'

            const copyBtn = document.createElement('button')
            copyBtn.type = 'button'
            copyBtn.className = 'tela-codeblock-copy'
            copyBtn.setAttribute('aria-label', 'Copy code')
            copyBtn.textContent = COPY_LABEL
            let resetTimer = 0
            // Keep the editor selection put when the button is pressed.
            copyBtn.addEventListener('mousedown', (e) => e.preventDefault())
            copyBtn.addEventListener('click', (e) => {
              e.preventDefault()
              const text = code.textContent ?? ''
              void navigator.clipboard?.writeText(text).catch(() => {})
              copyBtn.textContent = COPIED_LABEL
              copyBtn.dataset.copied = 'true'
              window.clearTimeout(resetTimer)
              resetTimer = window.setTimeout(() => {
                copyBtn.textContent = COPY_LABEL
                delete copyBtn.dataset.copied
              }, 1100)
            })

            header.appendChild(langLabel)
            header.appendChild(copyBtn)

            const dom = document.createElement('div')
            dom.className = 'tela-codeblock'
            if (lang) dom.dataset.language = lang
            dom.appendChild(header)
            dom.appendChild(pre)

            return {
              dom,
              contentDOM: code,
              update: (updated) => {
                if (updated.type !== node.type) return false
                const newLang = (updated.attrs.language as string) || ''
                if (newLang) {
                  dom.dataset.language = newLang
                  pre.dataset.language = newLang
                } else {
                  delete dom.dataset.language
                  delete pre.dataset.language
                }
                langLabel.textContent = newLang || 'text'
                return true
              },
              ignoreMutation: (m) => {
                // Header is ours — never let its mutations reconcile into PM.
                if (header.contains(m.target as Node)) return true
                return m.type === 'attributes' && m.target === dom
              },
              destroy: () => window.clearTimeout(resetTimer),
            }
          },
        },
      },
    }),
)
