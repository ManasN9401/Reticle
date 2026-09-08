import { useEffect, useRef, useState } from 'react'
import type { RuntimeEvent } from '@shared/events'
import { replayTo, type Run } from '@shared/projection'
import { bridge } from '@/state/bridge'
import { useActiveRun, useStudio } from '@/state/store'

/**
 * Time-travel debugging (RFC-022 §9).
 *
 * The projection is a pure fold over an ordered event log and `EventID` is a
 * monotonic counter, so "what did the graph look like at event N" is just a
 * replay. The buffered events live in the main process; they are fetched once,
 * lazily, the first time the user actually scrubs — there is no reason to ship
 * 50k events across IPC for a session that never uses the scrubber.
 */
export function useScrubbedRun(): { run: Run | undefined; replaying: boolean } {
  const liveRun = useActiveRun()
  const scrubEventId = useStudio((s) => s.scrubEventId)
  const selectedExecId = useStudio((s) => s.selectedExecId)

  const eventsRef = useRef<RuntimeEvent[] | null>(null)
  const [replayed, setReplayed] = useState<{key:string, run:Run|undefined} | undefined>(undefined)
  const replayKey = `${selectedExecId ?? liveRun?.execId}:${scrubEventId}`

  useEffect(() => {
    if (scrubEventId === null) {
      return
    }

    let cancelled = false

    const compute = (events: RuntimeEvent[]) => {
      if (cancelled) return
      const state = replayTo(events, scrubEventId)
      const execId = selectedExecId ?? liveRun?.execId
      setReplayed({key: replayKey, run: execId ? state.runs[execId] : undefined})
    }

    if (eventsRef.current) {
      const cached = eventsRef.current
      void Promise.resolve().then(() => compute(cached))
      return () => {
        cancelled = true
      }
    }

    void bridge?.projection.events().then((events) => {
      if (cancelled) return
      eventsRef.current = events
      compute(events)
    })

    return () => {
      cancelled = true
    }
  }, [scrubEventId, selectedExecId, liveRun?.execId, replayKey])

  // Invalidate the cache when live events arrive, so resuming and re-scrubbing
  // does not replay a stale log.
  const eventCount = useStudio((s) => s.projection.eventCount)
  useEffect(() => {
    if (scrubEventId === null) eventsRef.current = null
  }, [eventCount, scrubEventId])

  if (scrubEventId === null) return { run: liveRun, replaying: false }
  return { run: replayed?.key === replayKey ? replayed.run : undefined, replaying: true }
}
