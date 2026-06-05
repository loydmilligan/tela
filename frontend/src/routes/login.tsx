import { useState } from 'react'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { ApiError } from '../lib/api'
import {
  sanitizeNextPath,
  useLogin,
  useResendVerification,
} from '../lib/queries/auth'
import { Button } from '../components/ui/button'
import {
  Card,
  CardBody,
  CardDescription,
  CardHeader,
  CardTitle,
} from '../components/ui/card'
import { Input } from '../components/ui/input'
import { AuthShell, AuthField, AuthFooterLink } from '../components/app/AuthShell'

export function LoginPage() {
  const [identifier, setIdentifier] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  // When login fails because the account's email isn't confirmed, we surface a
  // dedicated "resend" affordance instead of the generic error.
  const [unverified, setUnverified] = useState(false)
  const [resent, setResent] = useState(false)
  const login = useLogin()
  const resend = useResendVerification()
  const navigate = useNavigate()
  const search = useSearch({ from: '/login' }) as { next?: string }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const id = identifier.trim()
    if (!id || !password) {
      setError('Email or username and password are required.')
      return
    }
    setError(null)
    setUnverified(false)
    setResent(false)
    try {
      await login.mutateAsync({ identifier: id, password })
      const next = sanitizeNextPath(search.next) ?? '/'
      // The WorkOS OAuth bridge (/oauth/workos/login) is a BACKEND route, not an
      // SPA route — hard-navigate so the browser hits the server (which completes
      // the Connect flow), not the client router.
      if (next.startsWith('/oauth/')) {
        window.location.assign(next)
        return
      }
      // Cast: `next` is an in-app path validated by sanitizeNextPath; the
      // router's typed route tree doesn't know it as a literal.
      void navigate({ to: next as never })
    } catch (err) {
      if (err instanceof ApiError && err.code === 'email_unverified') {
        setUnverified(true)
      } else if (err instanceof ApiError && err.status === 401) {
        setError('Invalid credentials.')
      } else if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Something went wrong. Please try again.')
      }
    }
  }

  async function handleResend() {
    try {
      await resend.mutateAsync(identifier.trim())
      setResent(true)
    } catch {
      setResent(true) // endpoint is always-202; treat any outcome as "sent"
    }
  }

  return (
    <AuthShell>
      <Card className="w-full max-w-[24rem]">
        <CardHeader>
          <CardTitle className="text-[length:var(--text-2xl)]">
            Sign in
          </CardTitle>
          <CardDescription>
            Enter your tela account credentials.
          </CardDescription>
        </CardHeader>
        <CardBody>
          <form
            onSubmit={handleSubmit}
            className="flex flex-col gap-[var(--space-4)]"
            noValidate
          >
            <AuthField id="login-identifier" label="Email or username">
              <Input
                id="login-identifier"
                autoFocus
                autoComplete="username"
                value={identifier}
                onChange={(e) => setIdentifier(e.target.value)}
                aria-invalid={error != null}
              />
            </AuthField>
            <AuthField
              id="login-password"
              label="Password"
              labelSlot={
                <Link
                  to="/forgot-password"
                  className="text-[length:var(--text-sm)] text-[var(--text-muted)] no-underline hover:text-[var(--text-primary)] hover:underline"
                >
                  Forgot password?
                </Link>
              }
            >
              <Input
                id="login-password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                aria-invalid={error != null}
              />
            </AuthField>
            {unverified ? (
              <div
                role="alert"
                className="flex flex-col gap-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-muted)]"
              >
                {resent ? (
                  <p className="m-0">
                    If that account needs confirming, a new link is on its way.
                  </p>
                ) : (
                  <>
                    <p className="m-0 text-[var(--danger)]">
                      Confirm your email before signing in.
                    </p>
                    <button
                      type="button"
                      onClick={() => void handleResend()}
                      disabled={resend.isPending}
                      className="self-start bg-transparent border-none p-0 text-[var(--accent)] underline cursor-pointer disabled:opacity-60"
                    >
                      {resend.isPending
                        ? 'Sending…'
                        : 'Resend confirmation email'}
                    </button>
                  </>
                )}
              </div>
            ) : null}
            {error ? (
              <p
                role="alert"
                className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
              >
                {error}
              </p>
            ) : null}
            <Button
              type="submit"
              variant="primary"
              size="lg"
              disabled={login.isPending}
            >
              {login.isPending ? 'Signing in…' : 'Sign in'}
            </Button>
          </form>
          <AuthFooterLink>
            New to tela?{' '}
            <Link
              to="/register"
              className="text-[var(--accent)] no-underline hover:underline"
            >
              Create an account
            </Link>
          </AuthFooterLink>
        </CardBody>
      </Card>
    </AuthShell>
  )
}
