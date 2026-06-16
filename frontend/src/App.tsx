import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { TooltipProvider } from './components/ui/tooltip'
import { ErrorBoundary } from './components/app/ErrorBoundary'
import { queryClient } from './lib/queryClient'
import { router } from './routes/router'

function App() {
  return (
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider delayDuration={150}>
          <RouterProvider router={router} />
        </TooltipProvider>
      </QueryClientProvider>
    </ErrorBoundary>
  )
}

export default App
