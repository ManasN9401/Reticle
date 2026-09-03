import { Copy, Minus, Square, X } from 'lucide-react'
import { cn } from '@/design/cn'
import { StatusPip } from '@/design/primitives'
import { formatDuration, runStatusVar } from '@/design/status'
import { bridge } from '@/state/bridge'
import { useActiveRun, useStudio } from '@/state/store'
import { useNow } from '@/state/useNow'
import { runTotals } from '@shared/projection'
import { MenuBar } from './MenuBar'
import { ReticleMark } from './ReticleMark'

/**
 * Custom title bar. The window is frameless, so this owns both the drag region
 * and the window controls — and it is the surface where the previous build's
 * dead-preload bug was most visible.
 */
export function TitleBar() {
  const maximized = useStudio((s) => s.windowState.maximized)
  const focused = useStudio((s) => s.windowState.focused)
  const run = useActiveRun()
  const totals = runTotals(run)
  // Ticks only while a run is in flight, so the elapsed readout actually moves.
  const now = useNow(run?.status === 'running')

  const elapsed =
    run?.startedAt !== undefined
      ? formatDuration((run.endedAt ?? now) - run.startedAt)
      : undefined

  return (
    <header
      className={cn(
        'drag relative z-50 flex h-[var(--h-titlebar)] shrink-0 items-center border-b border-line-1 bg-bg-1',
        !focused && 'text-fg-3',
      )}
    >
      <div className="flex w-[var(--h-rail)] shrink-0 items-center justify-center text-accent">
        <ReticleMark size={15} />
      </div>

      <MenuBar />

      {/*
        Centred run summary. Absolute so it stays optically centred regardless
        of menu width, and held back until xl — below that the menu bar and the
        window controls would crowd it, and the status bar already carries the
        same numbers.
      */}
      <div className="pointer-events-none absolute left-1/2 hidden -translate-x-1/2 items-center gap-2.5 xl:flex">
        {run ? (
          <>
            <StatusPip color={runStatusVar(run.status)} pulse={run.status === 'running'} />
            <span className="mono text-fg-2">{run.execId}</span>
            <span className="text-fg-4">·</span>
            <span className="num text-xs text-fg-3">
              {totals.done}/{totals.total} nodes
            </span>
            {totals.failed > 0 ? (
              <span className="num text-xs text-st-failed">{totals.failed} failed</span>
            ) : null}
            {totals.waiting > 0 ? (
              <span className="num text-xs text-st-waiting">{totals.waiting} waiting</span>
            ) : null}
            {elapsed ? (
              <>
                <span className="text-fg-4">·</span>
                <span className="num mono text-fg-3">{elapsed}</span>
              </>
            ) : null}
          </>
        ) : (
          <span className="text-xs text-fg-4">No active run</span>
        )}
      </div>

      <div className="no-drag ml-auto flex h-full items-stretch">
        <WindowButton label="Minimize" onClick={() => bridge?.window.minimize()}>
          <Minus size={14} strokeWidth={1.75} />
        </WindowButton>
        <WindowButton
          label={maximized ? 'Restore' : 'Maximize'}
          onClick={() => bridge?.window.maximize()}
        >
          {maximized ? (
            <Copy size={12} strokeWidth={1.75} />
          ) : (
            <Square size={11} strokeWidth={1.75} />
          )}
        </WindowButton>
        <WindowButton label="Close" danger onClick={() => bridge?.window.close()}>
          <X size={15} strokeWidth={1.75} />
        </WindowButton>
      </div>
    </header>
  )
}

function WindowButton({
  label,
  onClick,
  danger,
  children,
}: {
  label: string
  onClick: () => void
  danger?: boolean
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className={cn(
        'flex w-[46px] items-center justify-center text-fg-3',
        '[transition-property:background-color,color] duration-[var(--dur-fast)] ease-[var(--ease-out-quint)]',
        'focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-2',
        danger ? 'hover:bg-[#c4302b] hover:text-white' : 'hover:bg-bg-3 hover:text-fg-1',
      )}
    >
      {children}
    </button>
  )
}
