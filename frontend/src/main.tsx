import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import 'katex/dist/katex.min.css'
import './styles/index.css'
import { subscribeToAuthRequired } from './lib/api'
import { installGlobalErrorReporting, reloadOnceForStaleChunk } from './lib/client-errors'
import { initTheme } from './lib/theme'
import { authKeys } from './lib/queries/auth'
import { queryClient } from './lib/queryClient'
import { router } from './routes/router'
import App from './App.tsx'

initTheme()

// Capture uncaught exceptions + unhandled promise rejections globally and beacon
// them to the backend so client-side crashes are visible (admin Events feed +
// Prometheus). Installed before render so an error during startup is caught.
installGlobalErrorReporting()

// Stale-chunk recovery. After a frontend redeploy the hashed lazy-chunk
// filenames change; a tab still running the old bundle 404s when it tries to
// import a now-gone chunk. Vite fires `vite:preloadError` when the dynamic
// import() itself fails; the failed <link rel=modulepreload>/<script> 404s that
// precede it are handled by the resource listener in client-errors.ts. Both
// funnel to the same guarded one-shot reload so a stale tab picks up the fresh
// build instead of crashing the route.
window.addEventListener('vite:preloadError', () => reloadOnceForStaleChunk())

// Mid-session 401 handler. api.ts fires `tela:auth-required` after a non-auth
// endpoint comes back 401 (cookie expired, user deactivated, etc.). We clear
// the cached identity and bounce to /login?next=<current> so the form
// round-trips the user back where they were.
subscribeToAuthRequired((detail) => {
  // If we're already on /login, don't loop. (api.ts also skips auth paths so
  // this rarely fires there, but defence in depth.)
  if (window.location.pathname.startsWith('/login')) return
  queryClient.setQueryData(authKeys.me(), null)
  void router.navigate({ to: '/login', search: { next: detail.next } })
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
