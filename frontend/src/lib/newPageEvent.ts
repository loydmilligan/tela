// Tiny event bus for "open new-page dialog" requests. The dialog lives in
// AppCommandHost (sibling of RouterProvider so it can reach the query cache),
// but the sidebar "+ New page" button and the M5.2d broken-wikilink click
// handler both live deep inside the route tree. A window CustomEvent is the
// smallest bridge — matches the pattern already used for theme changes (see
// theme.ts).

const OPEN_NEW_PAGE_EVENT = 'tela:open-new-page'

export interface OpenNewPageOptions {
  // Pre-seed the dialog's title input. Used by the broken-wikilink click
  // handler to carry the dead link's text into the create flow (M5.2d).
  prefillTitle?: string
}

export function emitOpenNewPage(opts?: OpenNewPageOptions): void {
  if (typeof window === 'undefined') return
  window.dispatchEvent(
    new CustomEvent<OpenNewPageOptions>(OPEN_NEW_PAGE_EVENT, {
      detail: opts ?? {},
    }),
  )
}

export function subscribeToOpenNewPage(
  cb: (opts: OpenNewPageOptions) => void,
): () => void {
  if (typeof window === 'undefined') return () => {}
  function handler(e: Event) {
    const detail = (e as CustomEvent<OpenNewPageOptions | undefined>).detail
    cb(detail ?? {})
  }
  window.addEventListener(OPEN_NEW_PAGE_EVENT, handler)
  return () => window.removeEventListener(OPEN_NEW_PAGE_EVENT, handler)
}
