import { useEffect } from 'react'
import type { Decorator, Preview } from '@storybook/react-vite'
import { TooltipProvider } from '../src/components/ui/tooltip'
import 'katex/dist/katex.min.css'
import '../src/styles/index.css'

const withTheme: Decorator = (Story, context) => {
  const theme = (context.globals.theme as string) || 'light'
  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
  }, [theme])
  return (
    <TooltipProvider delayDuration={150}>
      <div
        style={{
          minHeight: '100vh',
          padding: 'var(--space-6)',
          background: 'var(--surface-1)',
          color: 'var(--text-primary)',
          fontFamily: 'var(--font-sans)',
          fontSize: 'var(--text-base)',
          lineHeight: 'var(--leading-normal)',
          transition:
            'background-color var(--duration-base) var(--ease-out), color var(--duration-base) var(--ease-out)',
        }}
      >
        <Story />
      </div>
    </TooltipProvider>
  )
}

const preview: Preview = {
  parameters: {
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    a11y: {
      test: 'todo',
    },
    backgrounds: { disable: true },
  },
  globalTypes: {
    theme: {
      description: 'Token-driven theme applied to data-theme on the preview root.',
      defaultValue: 'light',
      toolbar: {
        title: 'Theme',
        icon: 'paintbrush',
        items: [
          { value: 'light', title: 'Light' },
          { value: 'dark', title: 'Dark' },
          { value: 'warm', title: 'Warm' },
        ],
        dynamicTitle: true,
      },
    },
  },
  initialGlobals: { theme: 'light' },
  decorators: [withTheme],
}

export default preview
