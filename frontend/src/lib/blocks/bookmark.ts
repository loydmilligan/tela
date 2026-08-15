// Bookmark-card data + DOM, Milkdown-free. SINGLE SOURCE shared by the editor
// (milkdown-embed.ts nodeView, milkdown-bookshelf.ts) and the read-only view
// renderer (MarkdownView's card components) — same fetch, same cache, same
// card structure, so edit and view can't drift. See docs/view-edit-split.md.

export interface BookmarkMeta {
  url: string
  title: string
  description: string
  site_name: string
  favicon: string
  image: string
}

export function bookmarkHost(raw: string): string {
  try {
    return new URL(raw).hostname.replace(/^www\./, '')
  } catch {
    return raw
  }
}

// Client-side unfurl cache + in-flight dedupe: a bookshelf of N cards must not
// fire N duplicate fetches per mount, and revisiting a page should be free.
// The backend caches too (24h); this layer only spans the SPA session.
const metaCache = new Map<string, BookmarkMeta>()
const inFlight = new Map<string, Promise<BookmarkMeta>>()

function emptyMeta(url: string): BookmarkMeta {
  return { url, title: '', description: '', site_name: '', favicon: '', image: '' }
}

export function fetchBookmarkMeta(url: string): Promise<BookmarkMeta> {
  const cached = metaCache.get(url)
  if (cached) return Promise.resolve(cached)
  const pending = inFlight.get(url)
  if (pending) return pending
  // Raw fetch, not api(): same convention as the paste-unfurl plugin — a 401
  // here must not trigger the global login redirect mid-render.
  const p = fetch(`/api/unfurl?url=${encodeURIComponent(url)}`, {
    credentials: 'include',
  })
    .then(async (res) => {
      if (!res.ok) return emptyMeta(url)
      const data = (await res.json()) as Partial<BookmarkMeta>
      return {
        url,
        title: (data.title ?? '').trim(),
        description: (data.description ?? '').trim(),
        site_name: (data.site_name ?? '').trim(),
        favicon: (data.favicon ?? '').trim(),
        image: (data.image ?? '').trim(),
      }
    })
    .catch(() => emptyMeta(url))
    .then((meta) => {
      metaCache.set(url, meta)
      inFlight.delete(url)
      return meta
    })
  inFlight.set(url, p)
  return p
}

// Test seam: pre-seed the cache so stories/tests render cards without a live
// backend.
export function primeBookmarkMeta(meta: BookmarkMeta): void {
  metaCache.set(meta.url, meta)
}

// ── vanilla card DOM (editor nodeViews are plain-DOM by house style) ────────

export type BookmarkVariant = 'card' | 'row'

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className: string,
  parent?: HTMLElement,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag)
  if (className) node.className = className
  parent?.appendChild(node)
  return node
}

// Build the card skeleton synchronously (URL-only), then fill it when the
// unfurl resolves — the editor nodeView must return DOM immediately.
export function buildBookmarkDOM(url: string, variant: BookmarkVariant): HTMLElement {
  const root = el('a', variant === 'row' ? 'tela-bookmark tela-bookmark-row' : 'tela-bookmark')
  root.href = url
  root.target = '_blank'
  root.rel = 'noopener noreferrer nofollow'
  const media = el('span', 'tela-bookmark-media', root)
  const body = el('span', 'tela-bookmark-body', root)
  const title = el('span', 'tela-bookmark-title', body)
  title.textContent = bookmarkHost(url)
  const desc = el('span', 'tela-bookmark-desc', body)
  desc.hidden = true
  const site = el('span', 'tela-bookmark-site', body)
  const favicon = el('img', 'tela-bookmark-favicon', site)
  favicon.alt = ''
  favicon.loading = 'lazy'
  favicon.hidden = true
  favicon.onerror = () => {
    favicon.hidden = true
  }
  const host = el('span', 'tela-bookmark-host', site)
  host.textContent = bookmarkHost(url)
  media.hidden = true

  void fetchBookmarkMeta(url).then((meta) => applyBookmarkMeta(root, meta))
  return root
}

export function applyBookmarkMeta(root: HTMLElement, meta: BookmarkMeta): void {
  const q = (cls: string) => root.querySelector<HTMLElement>(`.${cls}`)
  const title = q('tela-bookmark-title')
  if (title) title.textContent = meta.title || bookmarkHost(meta.url)
  const desc = q('tela-bookmark-desc')
  if (desc) {
    desc.textContent = meta.description
    desc.hidden = !meta.description
  }
  const host = q('tela-bookmark-host')
  if (host) host.textContent = meta.site_name || bookmarkHost(meta.url)
  const favicon = q('tela-bookmark-favicon') as HTMLImageElement | null
  if (favicon && meta.favicon) {
    favicon.src = meta.favicon
    favicon.hidden = false
  }
  const media = q('tela-bookmark-media')
  if (media) {
    if (meta.image && !root.classList.contains('tela-bookmark-row')) {
      media.hidden = false
      media.style.backgroundImage = `url("${meta.image.replaceAll('"', '%22')}")`
    } else {
      media.hidden = true
    }
  }
}

// ── bookshelf directive helpers (Milkdown-free; used by the editor schema AND
// the view renderer — the view must never import the editor chunk) ──────────

export interface BookshelfDirectiveNode {
  type: string
  name?: string
  url?: string
  value?: string
  attributes?: Record<string, string>
  children?: BookshelfDirectiveNode[]
}

export function bookshelfUrlsFromDirective(node: BookshelfDirectiveNode): string[] {
  const urls: string[] = []
  const walk = (n: BookshelfDirectiveNode) => {
    if (n.type === 'link' && n.url) {
      urls.push(n.url)
      return // don't descend into the link's text
    }
    if (n.type === 'text' && n.value) {
      // Bare URLs that didn't autolink (rare — the gfm autolink normally
      // catches them inside the directive body).
      for (const m of n.value.matchAll(/https?:\/\/[^\s<>]+/g)) urls.push(m[0])
      return
    }
    n.children?.forEach(walk)
  }
  node.children?.forEach(walk)
  return [...new Set(urls)]
}
