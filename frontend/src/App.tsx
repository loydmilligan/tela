import { ThemeSwitcher } from './components/ThemeSwitcher'

function App() {
  return (
    <main
      style={{
        minHeight: '100vh',
        background: 'var(--surface-1)',
        color: 'var(--text-primary)',
        padding: 'var(--space-8) var(--space-6)',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        gap: 'var(--space-7)',
      }}
    >
      <header
        style={{
          display: 'flex',
          width: '100%',
          maxWidth: '640px',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 'var(--space-4)',
        }}
      >
        <h1
          style={{
            margin: 0,
            fontFamily: 'var(--font-sans)',
            fontSize: 'var(--text-3xl)',
            lineHeight: 'var(--leading-tight)',
            letterSpacing: '-0.02em',
            color: 'var(--text-primary)',
          }}
        >
          tela
        </h1>
        <ThemeSwitcher />
      </header>

      <section
        style={{
          width: '100%',
          maxWidth: '640px',
          background: 'var(--surface-2)',
          border: '1px solid var(--border-subtle)',
          borderRadius: 'var(--radius-lg)',
          padding: 'var(--space-7)',
          display: 'flex',
          flexDirection: 'column',
          gap: 'var(--space-4)',
        }}
      >
        <h2
          style={{
            margin: 0,
            fontFamily: 'var(--font-sans)',
            fontSize: 'var(--text-xl)',
            lineHeight: 'var(--leading-tight)',
            color: 'var(--text-primary)',
          }}
        >
          Design tokens demo
        </h2>
        <p
          style={{
            margin: 0,
            fontFamily: 'var(--font-sans)',
            fontSize: 'var(--text-base)',
            lineHeight: 'var(--leading-relaxed)',
            color: 'var(--text-muted)',
          }}
        >
          This card and every element on the page reads from semantic CSS
          custom properties. Flip the theme switcher above to re-skin the
          surface, text, and accent in real time. Your choice is persisted to
          localStorage and restored on reload.
        </p>
        <div
          style={{
            display: 'flex',
            gap: 'var(--space-3)',
            alignItems: 'center',
          }}
        >
          <button
            type="button"
            style={{
              padding: 'var(--space-3) var(--space-5)',
              fontFamily: 'var(--font-sans)',
              fontSize: 'var(--text-sm)',
              lineHeight: 'var(--leading-tight)',
              borderRadius: 'var(--radius-md)',
              border: '1px solid transparent',
              cursor: 'pointer',
              background: 'var(--accent)',
              color: 'var(--accent-fg)',
              transition:
                'background-color var(--duration-fast) var(--ease-out)',
            }}
          >
            Primary action
          </button>
          <span
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 'var(--text-xs)',
              color: 'var(--text-muted)',
            }}
          >
            radius-md · accent · accent-fg
          </span>
        </div>
      </section>
    </main>
  )
}

export default App
