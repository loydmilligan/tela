import { useEffect, useRef, useState } from 'react'
import { ShareError, useShareAuth } from '../../lib/queries/share'
import { ThemeSwitcher } from '../ThemeSwitcher'
import { Button } from '../ui/button'
import {
  Card,
  CardBody,
  CardDescription,
  CardHeader,
  CardTitle,
} from '../ui/card'
import { Input } from '../ui/input'

interface SharePasswordScreenProps {
  token: string
}

export function SharePasswordScreen({ token }: SharePasswordScreenProps) {
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  // Rate-limit countdown. When > 0, the submit button is disabled and the
  // status copy reflects the remaining seconds. Driven by the 429 response's
  // Retry-After header on the failing auth attempt.
  const [retryAfter, setRetryAfter] = useState(0)
  const auth = useShareAuth(token)
  const inputRef = useRef<HTMLInputElement>(null)

  // Focus on mount so a user landing on the share URL with the password
  // screen can start typing immediately without reaching for the mouse.
  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  useEffect(() => {
    if (retryAfter <= 0) return
    const t = window.setInterval(() => {
      setRetryAfter((s) => (s > 0 ? s - 1 : 0))
    }, 1000)
    return () => window.clearInterval(t)
  }, [retryAfter])

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (retryAfter > 0) return
    setError(null)
    try {
      await auth.mutateAsync(password)
      // Success — the mutation invalidates useShareRoot, which refetches with
      // the freshly minted cookie and the parent will swap in the ShareReader.
    } catch (err) {
      if (err instanceof ShareError) {
        if (err.kind === 'rate_limited') {
          setRetryAfter(err.retryAfter ?? 60)
          setError('Too many attempts. Try again shortly.')
          return
        }
        if (err.kind === 'invalid_password') {
          setError('Incorrect password.')
          return
        }
        if (err.kind === 'not_found') {
          setError('This share link is no longer available.')
          return
        }
        if (err.kind === 'bad_request') {
          setError('This share link no longer requires a password.')
          return
        }
        setError(err.message || 'Something went wrong.')
        return
      }
      setError('Something went wrong. Please try again.')
    }
  }

  const submitDisabled =
    auth.isPending || retryAfter > 0 || password.length === 0

  return (
    <div className="min-h-screen flex flex-col bg-[var(--surface-1)] text-[var(--text-primary)]">
      <header className="flex items-center justify-between px-[var(--space-6)] py-[var(--space-3)] border-b border-[var(--border-subtle)] shrink-0">
        <h1 className="m-0 text-[length:var(--text-lg)] leading-[var(--leading-tight)] font-[family-name:var(--font-sans)]">
          tela
        </h1>
        <ThemeSwitcher />
      </header>
      <main className="flex-1 flex items-center justify-center p-[var(--space-7)]">
        <Card className="w-full max-w-[24rem]">
          <CardHeader>
            <CardTitle className="text-[length:var(--text-2xl)]">
              Password required
            </CardTitle>
            <CardDescription>
              This share is protected. Enter the password to view it.
            </CardDescription>
          </CardHeader>
          <CardBody>
            <form
              onSubmit={handleSubmit}
              className="flex flex-col gap-[var(--space-4)]"
              noValidate
            >
              <div className="flex flex-col gap-[var(--space-2)]">
                <label
                  htmlFor="share-password"
                  className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
                >
                  Password
                </label>
                <Input
                  id="share-password"
                  ref={inputRef}
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  aria-invalid={error != null}
                  disabled={retryAfter > 0}
                />
              </div>
              {error ? (
                <p
                  role="alert"
                  className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
                >
                  {error}
                  {retryAfter > 0 ? ` (${retryAfter}s)` : ''}
                </p>
              ) : null}
              {auth.isPending ? (
                <p
                  role="status"
                  className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]"
                >
                  Checking…
                </p>
              ) : null}
              <Button
                type="submit"
                variant="primary"
                size="lg"
                disabled={submitDisabled}
              >
                {retryAfter > 0
                  ? `Try again in ${retryAfter}s`
                  : auth.isPending
                    ? 'Checking…'
                    : 'View share'}
              </Button>
            </form>
          </CardBody>
        </Card>
      </main>
    </div>
  )
}
