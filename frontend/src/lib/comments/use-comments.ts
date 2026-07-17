import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'

export interface Comment {
  id: number
  page_id: number
  parent_id: number | null
  author_id: number
  author_username: string
  body: string
  anchor_prefix?: string
  anchor_exact?: string
  anchor_suffix?: string
  resolved: boolean
  resolved_at?: string
  resolved_by?: number
  resolved_by_username?: string
  created_at: string
  updated_at: string
  /**
   * Structured metadata for a change-comment (migration 0069) — the queryable
   * bag behind `query{ target: comments }`. Conventional keys: change_summary,
   * type, status, version. Absent when the comment carries none (the backend
   * omits an empty bag).
   */
  props?: Record<string, unknown>
}

export interface CommentThread {
  root: Comment
  replies: Comment[]
}

interface ListResponse {
  threads: CommentThread[]
}

export const commentKeys = {
  all: ['comments'] as const,
  page: (pageId: number) => [...commentKeys.all, 'page', pageId] as const,
}

export interface UseCommentsArgs {
  pageId: number | null | undefined
  // Skip the query entirely (used to gate viewer-role users out of the
  // comments surface — they get 403 from the backend if we ask).
  enabled?: boolean
}

export function useComments({ pageId, enabled = true }: UseCommentsArgs) {
  // Always fetch with include_resolved=true; #74 will filter client-side using
  // a localStorage-persisted toggle. Fetching everything keeps the open-thread
  // count + the resolved-thread count both derivable from the same cache.
  return useQuery({
    queryKey: pageId != null ? commentKeys.page(pageId) : commentKeys.page(-1),
    queryFn: async () => {
      const { threads } = await api<ListResponse>(
        `/api/pages/${pageId}/comments?include_resolved=true`,
      )
      return threads
    },
    enabled: enabled && pageId != null,
  })
}

export interface CreateCommentInput {
  body: string
  parent_id?: number | null
  anchor_prefix?: string
  anchor_exact?: string
  anchor_suffix?: string
  /** Structured change-comment metadata; see Comment.props. */
  props?: Record<string, unknown>
}

interface OptimisticContext {
  previous: CommentThread[] | undefined
  tempId: number
}

// Optimistic id space: negative, monotonically decreasing so we never collide
// with the backend's positive AUTOINCREMENT.
function makeOptimisticId(): number {
  return -Date.now()
}

export function useCreateComment(pageId: number, me: { id: number; username: string }) {
  const qc = useQueryClient()
  return useMutation<Comment, Error, CreateCommentInput, OptimisticContext>({
    mutationFn: async (input) => {
      const { comment } = await api<{ comment: Comment }>(
        `/api/pages/${pageId}/comments`,
        { method: 'POST', body: JSON.stringify(input) },
      )
      return comment
    },
    onMutate: async (input) => {
      const key = commentKeys.page(pageId)
      await qc.cancelQueries({ queryKey: key })
      const previous = qc.getQueryData<CommentThread[]>(key)
      const tempId = makeOptimisticId()
      const nowIso = new Date().toISOString().replace('T', ' ').slice(0, 19)
      const optimistic: Comment = {
        id: tempId,
        page_id: pageId,
        parent_id: input.parent_id ?? null,
        author_id: me.id,
        author_username: me.username,
        body: input.body,
        anchor_prefix: input.anchor_prefix,
        anchor_exact: input.anchor_exact,
        anchor_suffix: input.anchor_suffix,
        props: input.props,
        resolved: false,
        created_at: nowIso,
        updated_at: nowIso,
      }
      if (previous) {
        if (input.parent_id == null) {
          // Root — append (backend orders threads ASC by created_at, panel
          // will reverse on render so this lands "at the top" visually).
          qc.setQueryData<CommentThread[]>(key, [
            ...previous,
            { root: optimistic, replies: [] },
          ])
        } else {
          qc.setQueryData<CommentThread[]>(
            key,
            previous.map((t) =>
              t.root.id === input.parent_id
                ? { ...t, replies: [...t.replies, optimistic] }
                : t,
            ),
          )
        }
      }
      return { previous, tempId }
    },
    onError: (_err, _input, ctx) => {
      if (ctx?.previous) {
        qc.setQueryData(commentKeys.page(pageId), ctx.previous)
      }
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: commentKeys.page(pageId) })
    },
  })
}

export interface UpdateCommentInput {
  id: number
  // Mutually exclusive — backend 400s if both are sent in the same request.
  body?: string
  resolved?: boolean
}

export function useUpdateComment(pageId: number) {
  const qc = useQueryClient()
  return useMutation<Comment, Error, UpdateCommentInput>({
    mutationFn: async ({ id, ...patch }) => {
      const { comment } = await api<{ comment: Comment }>(
        `/api/comments/${id}`,
        { method: 'PATCH', body: JSON.stringify(patch) },
      )
      return comment
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: commentKeys.page(pageId) })
    },
  })
}

interface DeleteContext {
  previous: CommentThread[] | undefined
}

export function useDeleteComment(pageId: number) {
  const qc = useQueryClient()
  return useMutation<void, Error, { id: number }, DeleteContext>({
    mutationFn: async ({ id }) => {
      await api<void>(`/api/comments/${id}`, { method: 'DELETE' })
    },
    onMutate: async ({ id }) => {
      const key = commentKeys.page(pageId)
      await qc.cancelQueries({ queryKey: key })
      const previous = qc.getQueryData<CommentThread[]>(key)
      if (previous) {
        const next: CommentThread[] = []
        for (const t of previous) {
          if (t.root.id === id) continue
          next.push({
            ...t,
            replies: t.replies.filter((r) => r.id !== id),
          })
        }
        qc.setQueryData<CommentThread[]>(key, next)
      }
      return { previous }
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.previous) {
        qc.setQueryData(commentKeys.page(pageId), ctx.previous)
      }
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: commentKeys.page(pageId) })
    },
  })
}
