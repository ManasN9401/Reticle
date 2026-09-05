import { useEffect, useState } from 'react'

/**
 * A clock that re-renders on an interval.
 *
 * Elapsed-time readouts cannot call `Date.now()` during render: the component
 * only re-renders when its store slice changes, so a running node's timer would
 * sit frozen between events — exactly when the user most wants to see it move.
 *
 * Pass `active: false` when nothing is running so idle windows do not tick.
 */
export function useNow(active: boolean, intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (!active) return
    setNow(Date.now())
    const id = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(id)
  }, [active, intervalMs])

  return now
}
