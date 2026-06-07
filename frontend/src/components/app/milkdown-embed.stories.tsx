import type { Meta, StoryObj } from '@storybook/react-vite'
import { embedIframeSrc } from './milkdown-embed'

// Showcase the embed chrome: a responsive iframe frame for allowlisted
// providers, a link card for everything else. The real editor renders this via
// embedSchema.toDOM; here the iframe is swapped for a labelled placeholder so
// the story doesn't make a network call (the aspect-ratio frame is the point).

function EmbedFrame({ url }: { url: string }) {
  const src = embedIframeSrc(url)
  return (
    <div className="tela-milkdown">
      <div className="ProseMirror">
        {src ? (
          <div className="tela-embed" data-url={url} contentEditable={false}>
            {/* Placeholder stands in for the sandboxed <iframe src={src}> */}
            <div
              style={{
                width: '100%',
                height: '100%',
                display: 'grid',
                placeItems: 'center',
                color: 'var(--text-muted)',
                fontFamily: 'var(--font-mono)',
                fontSize: 'var(--text-xs)',
              }}
            >
              iframe → {src}
            </div>
          </div>
        ) : (
          <div
            className="tela-embed tela-embed-link"
            data-url={url}
            contentEditable={false}
          >
            <a href={url} target="_blank" rel="noopener noreferrer nofollow">
              {url}
            </a>
          </div>
        )}
      </div>
    </div>
  )
}

const meta: Meta<typeof EmbedFrame> = {
  title: 'App/Milkdown Embed',
  component: EmbedFrame,
  parameters: { layout: 'padded' },
}
export default meta

type Story = StoryObj<typeof EmbedFrame>

export const YouTube: Story = {
  args: { url: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ' },
}
export const Vimeo: Story = {
  args: { url: 'https://vimeo.com/76979871' },
}
export const Loom: Story = {
  args: { url: 'https://www.loom.com/share/abcdef0123456789' },
}
export const LinkFallback: Story = {
  name: 'Unknown provider — link card',
  args: { url: 'https://example.com/some/article' },
}
