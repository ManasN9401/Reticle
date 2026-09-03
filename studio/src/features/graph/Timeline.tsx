import { useCallback, useMemo, useRef } from 'react'
import { History, X } from 'lucide-react'
import { cn } from '@/design/cn'
import { IconButton } from '@/design/primitives'
import { useStudio } from '@/state/store'

/**
 * The run timeline.
 *
 * One tick per event, coloured by kind, with a draggable playhead. Dragging
 * replays the projection to that event id and the graph renders the state the
 * run was actually in at that moment — the thing that makes this an instrument
 * rather than a dashboard.
 */

const TICK_COLOR: Record<string, string> = {
  WorkerFailed: 'var(--color-st-failed)',
  TaskFailed: 'var(--color-st-failed)',
  WorkflowFailed: 'var(--color-st-failed)',
  WorkerCompleted: 'var(--color-st-done)',
  WorkflowCompleted: 'var(--color-st-done)',
  WorkerStarted: 'var(--color-st-running)',
  TaskDispatched: 'var(--color-st-running)',
  WorkflowStarted: 'var(--color-accent)',
  ArtifactsProduced: 'var(--color-fg-3)',
}

const DEFAULT_TICK = 'var(--color-line-2)'
/** Number of horizontal buckets. More than this is invisible at any panel width. */
const BUCKETS = 400

export function Timeline() {
  const timeline = useStudio((s) => s.timeline)
  const scrubEventId = useStudio((s) => s.scrubEventId)
  const setScrub = useStudio((s) => s.setScrub)
  const trackRef = useRef<HTMLDivElement>(null)

  const { buckets, minId, maxId } = useMemo(() => {
    if (timeline.length === 0) return { buckets: [], minId: 0, maxId: 0 }
    const min = timeline[0].id
    const max = timeline[timeline.length - 1].id
    const span = Math.max(1, max - min)

    // Each bucket keeps the most severe event in it, so a single failure in a
    // thousand successes is still visible.
    const out: (string | null)[] = new Array(BUCKETS).fill(null)
    const priority = (type: string) =>
      type.endsWith('Failed') ? 3 : type === 'WorkflowStarted' ? 2 : 1
    const best: number[] = new Array(BUCKETS).fill(0)

    for (const tick of timeline) {
      const index = Math.min(BUCKETS - 1, Math.floor(((tick.id - min) / span) * BUCKETS))
      const p = priority(tick.type)
      if (p >= best[index]) {
        best[index] = p
        out[index] = TICK_COLOR[tick.type] ?? DEFAULT_TICK
      }
    }
    return { buckets: out, minId: min, maxId: max }
  }, [timeline])

  const idAt = useCallback(
    (clientX: number): number => {
      const rect = trackRef.current?.getBoundingClientRect()
      if (!rect || rect.width === 0) return maxId
      const ratio = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width))
      return Math.round(minId + ratio * (maxId - minId))
    },
    [minId, maxId],
  )

  const onPointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    if (timeline.length === 0) return
    event.currentTarget.setPointerCapture(event.pointerId)
    setScrub(idAt(event.clientX))
  }

  const onPointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    if (!event.currentTarget.hasPointerCapture(event.pointerId)) return
    setScrub(idAt(event.clientX))
  }

  const onPointerUp = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
  }

  const playheadRatio =
    scrubEventId !== null && maxId > minId ? (scrubEventId - minId) / (maxId - minId) : 1

  return (
    <div className="flex h-7 shrink-0 items-center gap-2 border-t border-line-1 bg-bg-1 px-2">
      <History
        size={12}
        strokeWidth={1.7}
        className={cn('shrink-0', scrubEventId !== null ? 'text-st-waiting' : 'text-fg-4')}
      />

      <div
        ref={trackRef}
        role="slider"
        tabIndex={0}
        aria-label="Run timeline"
        aria-valuemin={minId}
        aria-valuemax={maxId}
        aria-valuenow={scrubEventId ?? maxId}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        onKeyDown={(event) => {
          if (event.key === 'ArrowLeft') {
            event.preventDefault()
            setScrub(Math.max(minId, (scrubEventId ?? maxId) - 1))
          } else if (event.key === 'ArrowRight') {
            event.preventDefault()
            const next = (scrubEventId ?? maxId) + 1
            setScrub(next >= maxId ? null : next)
          } else if (event.key === 'Escape') {
            setScrub(null)
          }
        }}
        className={cn(
          'relative h-4 min-w-0 flex-1 cursor-crosshair touch-none overflow-hidden rounded-[3px] bg-inset',
          'focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-1',
        )}
      >
        <div className="flex h-full w-full items-end">
          {buckets.map((color, index) => (
            <span
              key={index}
              className="h-full flex-1"
              style={{ backgroundColor: color ?? 'transparent' }}
            />
          ))}
        </div>

        {timeline.length > 0 ? (
          <span
            aria-hidden
            className={cn(
              'pointer-events-none absolute inset-y-0 w-px',
              scrubEventId !== null ? 'bg-st-waiting' : 'bg-accent/50',
            )}
            style={{ left: `${playheadRatio * 100}%` }}
          />
        ) : null}

        {timeline.length === 0 ? (
          <span className="absolute inset-0 flex items-center justify-center text-2xs text-fg-4">
            No events yet
          </span>
        ) : null}
      </div>

      <span className="num mono w-24 shrink-0 text-right text-2xs text-fg-4">
        {scrubEventId !== null ? `#${scrubEventId}` : `${timeline.length} events`}
      </span>

      {scrubEventId !== null ? (
        <IconButton label="Resume live view" size="sm" onClick={() => setScrub(null)}>
          <X size={13} strokeWidth={1.8} />
        </IconButton>
      ) : null}
    </div>
  )
}
