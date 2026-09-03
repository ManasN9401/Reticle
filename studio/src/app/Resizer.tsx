import { useCallback, useRef } from 'react'
import { cn } from '@/design/cn'

/**
 * A 1px divider with a 7px invisible grab area.
 *
 * The visible line stays hairline-thin so the chrome does not thicken on hover,
 * but the pointer target is comfortable — the same trick every editor uses for
 * splitters.
 */
export function Resizer({
  orientation,
  onResize,
  label,
  className,
}: {
  orientation: 'vertical' | 'horizontal'
  /** Called with the pointer delta in px since the drag started. */
  onResize: (delta: number) => void
  label: string
  className?: string
}) {
  const origin = useRef(0)
  const isVertical = orientation === 'vertical'

  const onPointerDown = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      event.preventDefault()
      const target = event.currentTarget
      target.setPointerCapture(event.pointerId)
      origin.current = isVertical ? event.clientX : event.clientY
    },
    [isVertical],
  )

  const onPointerMove = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      if (!event.currentTarget.hasPointerCapture(event.pointerId)) return
      const current = isVertical ? event.clientX : event.clientY
      onResize(current - origin.current)
      origin.current = current
    },
    [isVertical, onResize],
  )

  const onPointerUp = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
  }, [])

  return (
    <div
      role="separator"
      aria-label={label}
      aria-orientation={isVertical ? 'vertical' : 'horizontal'}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      className={cn(
        'group relative z-20 shrink-0 touch-none',
        isVertical ? 'w-px cursor-col-resize' : 'h-px cursor-row-resize',
        'bg-line-1',
        className,
      )}
    >
      <span
        className={cn(
          'absolute bg-accent opacity-0',
          '[transition-property:opacity] delay-100 duration-[var(--dur-base)]',
          'group-hover:opacity-100 group-active:opacity-100',
          isVertical ? 'inset-y-0 -inset-x-0' : '-inset-y-0 inset-x-0',
        )}
      />
      {/* Invisible, comfortable hit area. */}
      <span
        className={cn(
          'absolute',
          isVertical ? 'inset-y-0 -left-[3px] w-[7px]' : 'inset-x-0 -top-[3px] h-[7px]',
        )}
      />
    </div>
  )
}
