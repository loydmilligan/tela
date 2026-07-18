import { useQuery } from '@tanstack/react-query'
import { api } from '../api'

// Templates for the create-from-template picker (#12). A template is any page
// the caller can read with `props.template: true`. We reuse the props-query
// endpoint (GIN-indexed `props @> {template:true}`, access-gated) rather than a
// new endpoint — "everywhere" is one indexed lookup of a small set, not a scan,
// and returns props + title only (no body; the body is fetched on pick).

export interface TemplatePage {
  id: number
  space_id: number
  space_name: string
  title: string
  props: Record<string, unknown>
  updated_at: string
}

export function useTemplates(enabled = true) {
  return useQuery({
    queryKey: ['templates'],
    enabled,
    queryFn: async () => {
      const r = await api<{ pages: TemplatePage[] }>('/api/pages/query', {
        method: 'POST',
        body: JSON.stringify({ where: { template: true }, sort: 'title', limit: 200 }),
      })
      return r.pages ?? []
    },
    staleTime: 30_000,
  })
}
