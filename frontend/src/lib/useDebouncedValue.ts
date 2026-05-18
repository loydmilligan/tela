import { useEffect, useState } from 'react'

// Returns a debounced view of `value` — updates only after `delayMs` has
// elapsed since the most recent change. The fresh value beating an in-flight
// timer cancels and reschedules, so the consumer only ever sees a settled value.
export function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(value), delayMs)
    return () => window.clearTimeout(t)
  }, [value, delayMs])
  return debounced
}
