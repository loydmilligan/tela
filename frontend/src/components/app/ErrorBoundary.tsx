import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Button } from '../ui/button'
import { reportClientError } from '../../lib/client-errors'

interface Props {
  children: ReactNode
}

interface State {
  hasError: boolean
}

// Top-level React error boundary. A render-phase throw anywhere below it would
// otherwise unmount the whole tree to a blank white page with nothing logged;
// here we (1) beacon the error to /api/client-errors so it shows up in the admin
// Events feed, and (2) show a recoverable fallback instead of a dead page.
//
// Class component because React only exposes error boundaries via the
// getDerivedStateFromError / componentDidCatch lifecycle — there is no hook
// equivalent.
export class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false }

  static getDerivedStateFromError(): State {
    return { hasError: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    reportClientError({
      kind: 'react',
      message: error.message || error.name || 'render error',
      stack: error.stack,
      // componentStack points at the subtree that threw — the most useful
      // breadcrumb for an admin reading the report.
      component: info.componentStack?.split('\n', 3).join('\n').trim(),
    })
  }

  render() {
    if (!this.state.hasError) return this.props.children
    return (
      <div
        role="alert"
        className="flex min-h-screen flex-col items-center justify-center gap-[var(--space-4)] p-[var(--space-6)] text-center"
      >
        <div className="flex flex-col gap-[var(--space-2)]">
          <h1 className="m-0 text-[length:var(--text-xl)] font-semibold">
            Something went wrong
          </h1>
          <p className="m-0 max-w-[28rem] text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
            This part of the app hit an unexpected error. It's been reported.
            Reloading usually fixes it.
          </p>
        </div>
        <Button onClick={() => window.location.reload()}>Reload</Button>
      </div>
    )
  }
}
