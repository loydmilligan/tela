import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { TooltipProvider } from './components/ui/tooltip'
import { queryClient } from './lib/queryClient'
import { router } from './routes/router'

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={150}>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

export default App
