import { useState } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { ApiError } from '../lib/api'
import {
  sanitizeNextPath,
  useLogin,
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
import { ThemeSwitcher } from '../components/ThemeSwitcher'

export function LoginPage() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const login = useLogin()
  const navigate = useNavigate()
  const search = useSearch({ from: '/login' }) as { next?: string }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const u = username.trim()
    if (!u || !password) {
      setError('Username and password are required.')
      return
    }
    setError(null)
    try {
      await login.mutateAsync({ username: u, password })
      const next = sanitizeNextPath(search.next) ?? '/'
      // Cast: `next` is an in-app path validated by sanitizeNextPath; the
      // router's typed route tree doesn't know it as a literal.
      void navigate({ to: next as never })
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setError('Invalid credentials.')
      } else if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Something went wrong. Please try again.')
      }
    }
  }

  const submitDisabled = login.isPending

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
              <div className="flex flex-col gap-[var(--space-2)]">
                <label
                  htmlFor="login-username"
                  className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
                >
                  Username
                </label>
                <Input
                  id="login-username"
                  autoFocus
                  autoComplete="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  aria-invalid={error != null}
                />
              </div>
              <div className="flex flex-col gap-[var(--space-2)]">
                <label
                  htmlFor="login-password"
                  className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
                >
                  Password
                </label>
                <Input
                  id="login-password"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  aria-invalid={error != null}
                />
              </div>
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
                disabled={submitDisabled}
              >
                {login.isPending ? 'Signing in…' : 'Sign in'}
              </Button>
            </form>
          </CardBody>
        </Card>
      </main>
    </div>
  )
}
