// Tiny event bus for "any page mutation succeeded" notifications. Dispatched
// from useCreatePage / useUpdatePage / useMovePage / useDeletePage onSuccess
// handlers. Decouples the Orama tier-1 title index (orama-index.ts) from the
// React Query mutation pipeline — the index module subscribes once at boot and
// refreshes itself without queries.ts needing to know it exists.
//
// Mirrors the pattern used by newPageEvent.ts (window CustomEvent).

const PAGE_MUTATION_EVENT = 'tela:page-mutation'

export function emitPageMutation(): void {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent(PAGE_MUTATION_EVENT))
}

export function subscribeToPageMutation(cb: () => void): () => void {
  if (typeof window === 'undefined') return () => {}
  function handler() {
    cb()
  }
  window.addEventListener(PAGE_MUTATION_EVENT, handler)
  return () => window.removeEventListener(PAGE_MUTATION_EVENT, handler)
}
