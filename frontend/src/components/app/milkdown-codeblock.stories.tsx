import type { Meta, StoryObj } from '@storybook/react-vite'
import type { ReactNode } from 'react'

// Showcase the code-block chrome (language label + copy button). The editor
// renders the same class structure via the milkdown-codeblock.ts nodeView; here
// we render the static DOM inside a `.tela-milkdown .ProseMirror` wrapper so the
// scoped CSS applies without a Milkdown mount. Prism token classes are added by
// hand to preview the syntax palette.

interface CodeBlockPreviewProps {
  language: string
  children: ReactNode
}

function CodeBlockPreview({ language, children }: CodeBlockPreviewProps) {
  return (
    <div className="tela-milkdown">
      <div className="ProseMirror">
        <div className="tela-codeblock" data-language={language}>
          <div className="tela-codeblock-header" contentEditable={false}>
            <span className="tela-codeblock-lang">{language || 'text'}</span>
            <button type="button" className="tela-codeblock-copy">
              Copy
            </button>
          </div>
          <pre data-language={language}>
            <code data-language={language}>{children}</code>
          </pre>
        </div>
      </div>
    </div>
  )
}

const meta: Meta<typeof CodeBlockPreview> = {
  title: 'App/Milkdown Code Block',
  component: CodeBlockPreview,
  parameters: { layout: 'padded' },
}
export default meta

type Story = StoryObj<typeof CodeBlockPreview>

export const TypeScript: Story = {
  render: () => (
    <CodeBlockPreview language="typescript">
      <span className="token keyword">export</span>{' '}
      <span className="token keyword">function</span>{' '}
      <span className="token function">greet</span>
      <span className="token punctuation">(</span>name
      <span className="token punctuation">:</span>{' '}
      <span className="token keyword">string</span>
      <span className="token punctuation">)</span>{' '}
      <span className="token punctuation">{'{'}</span>
      {'\n  '}
      <span className="token keyword">return</span>{' '}
      <span className="token string">{'`Hello, ${name}`'}</span>
      {'\n'}
      <span className="token punctuation">{'}'}</span>
    </CodeBlockPreview>
  ),
}

export const NoLanguage: Story = {
  name: 'No language (text)',
  render: () => (
    <CodeBlockPreview language="">
      $ tela deploy --prod{'\n'}✓ built in 1.7s
    </CodeBlockPreview>
  ),
}

export const Copied: Story = {
  name: 'Copy button — copied state',
  render: () => (
    <div className="tela-milkdown">
      <div className="ProseMirror">
        <div className="tela-codeblock" data-language="bash">
          <div className="tela-codeblock-header" contentEditable={false}>
            <span className="tela-codeblock-lang">bash</span>
            <button type="button" className="tela-codeblock-copy" data-copied>
              Copied
            </button>
          </div>
          <pre data-language="bash">
            <code data-language="bash">npm run build</code>
          </pre>
        </div>
      </div>
    </div>
  ),
}
