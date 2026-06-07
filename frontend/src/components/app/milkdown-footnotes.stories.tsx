import type { Meta, StoryObj } from '@storybook/react-vite'

// Showcase footnote rendering. The gfm preset parses `[^n]` into
// <sup data-type="footnote_reference"> and `[^n]: …` into
// <dl data-type="footnote_definition">; editor.css styles the marker + list,
// and the reader layer (shown in the "Reader" story) adds the "Footnotes"
// section header + back-link. Static DOM inside the matching wrappers so the
// scoped CSS applies without a Milkdown mount.

function EditorFootnotes() {
  return (
    <div className="tela-milkdown">
      <div className="ProseMirror">
        <p>
          Markdown is the canonical format
          <sup data-type="footnote_reference" data-label="1">
            1
          </sup>{' '}
          and Yjs powers live collaboration
          <sup data-type="footnote_reference" data-label="2">
            2
          </sup>
          .
        </p>
        <dl data-type="footnote_definition" data-label="1">
          <dt>1</dt>
          <dd>
            <p>The page body is stored as markdown forever — no block table.</p>
          </dd>
        </dl>
        <dl data-type="footnote_definition" data-label="2">
          <dt>2</dt>
          <dd>
            <p>Imports allowed only in src/lib/collab/*.</p>
          </dd>
        </dl>
      </div>
    </div>
  )
}

function ReaderFootnotes() {
  return (
    <div className="tela-reader">
      <div className="tela-milkdown">
        <div className="ProseMirror">
          <p>
            Markdown is the canonical format
            <sup
              data-type="footnote_reference"
              data-label="1"
              className="reader-footnote-ref"
            >
              1
            </sup>
            .
          </p>
          <dl
            data-type="footnote_definition"
            data-label="1"
            className="reader-footnote-def reader-footnotes-start"
          >
            <dt>1</dt>
            <dd>
              <p>
                The page body is stored as markdown forever — no block table.
              </p>
              <a href="#fnref-1" className="reader-footnote-back">
                ↩
              </a>
            </dd>
          </dl>
        </div>
      </div>
    </div>
  )
}

const meta: Meta = {
  title: 'App/Milkdown Footnotes',
  parameters: { layout: 'padded' },
}
export default meta

type Story = StoryObj

export const Editor: Story = {
  name: 'Editor — base styling',
  render: () => <EditorFootnotes />,
}

export const Reader: Story = {
  name: 'Reader — Footnotes section + back-link',
  render: () => <ReaderFootnotes />,
}
