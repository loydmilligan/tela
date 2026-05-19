import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './styles/index.css'
import { subscribeToAuthRequired } from './lib/api'
import { initTheme } from './lib/theme'
import { authKeys } from './lib/queries/auth'
import { queryClient } from './lib/queryClient'
import { router } from './routes/router'
import App from './App.tsx'

initTheme()

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
