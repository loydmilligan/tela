import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { CommandHost } from './components/ui/command'
import { TooltipProvider } from './components/ui/tooltip'
import { queryClient } from './lib/queryClient'
import { router } from './routes/router'

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={150}>
        <RouterProvider router={router} />
        <CommandHost />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

export default App
