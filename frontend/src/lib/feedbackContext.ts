import { getTheme } from './theme'
import { queryClient } from './queryClient'
import { pageKeys } from './queries/pages'
import type { FeedbackContext } from './types'

// Snapshot the current app context to attach to a feedback submission, so the
// inbox can triage without asking the user a thing (Vercel/Geist auto-attach
// device + route the same way). The route bits come from the caller (the widget
// reads them from the router at render); we add theme/viewport/locale here and
// look the page title up from the query cache. The backend folds in source,
// app version, and user-agent — never trust the client for those.
export function collectFeedbackContext(route: {
  pathname: string
  spaceId: number | null
  pageId: number | null
}): FeedbackContext {
  const ctx: FeedbackContext = { route: route.pathname }
  if (route.spaceId != null) ctx.space_id = route.spaceId
  if (route.pageId != null) {
    ctx.page_id = route.pageId
    const page = queryClient.getQueryData<{ title?: string }>(
      pageKeys.detail(route.pageId),
    )
    const title = page?.title?.trim()
    if (title) ctx.page_title = title
  }
  ctx.theme = getTheme()
  if (typeof window !== 'undefined') {
    ctx.viewport = `${window.innerWidth}×${window.innerHeight}`
    ctx.locale = navigator.language
  }
  return ctx
}
