import {
  AlertTriangle,
  Cpu,
  Monitor,
  Moon,
  Play,
  Square,
  Sun,
  Wifi,
  WifiOff,
} from 'lucide-react'
import { cn } from '@/design/cn'
import { StatusPip } from '@/design/primitives'
import { connectionVar } from '@/design/status'
import {
  canStartForge,
  canStopForge,
  connect,
  disconnect,
  startForge,
  stopForge,
} from '@/state/actions'
import { bridge } from '@/state/bridge'
import { useActiveRun, useStudio } from '@/state/store'
import { nextTheme } from '@/state/theme'
import { runTotals } from '@shared/projection'
import { useUi } from '@/state/ui'

const PHASE_LABEL: Record<string, string> = {
  idle: 'Disconnected',
  connecting: 'Connecting',
  connected: 'Connected',
  reconnecting: 'Reconnecting',
  error: 'Error',
}

/**
 * The status bar is the app's honesty layer: connection, the managed forge
 * process, and what the graph currently holds. If something is not working,
 * this is where it says so rather than leaving a control mysteriously inert.
 */
export function StatusBar() {
  const connection = useStudio((s) => s.connection)
  const forge = useStudio((s) => s.forge)
  const logsTruncated = useStudio((s) => s.logsTruncated)
  const scrubEventId = useStudio((s) => s.scrubEventId)
  const setScrub = useStudio((s) => s.setScrub)
  const setPanelTab = useUi((s) => s.setPanelTab)
  const run = useActiveRun()
  const totals = runTotals(run)

  const connected = connection.phase === 'connected'

  return (
    <footer className="flex h-[var(--h-statusbar)] shrink-0 items-center gap-0.5 border-t border-line-1 bg-bg-1 px-1 text-xs text-fg-3">
      <StatusItem
        onClick={() => (connected ? disconnect() : connect())}
        title={
          connected
            ? 'Click to disconnect from the orchestrator'
            : 'Click to connect to the orchestrator'
        }
      >
        <StatusPip
          color={connectionVar(connection.phase)}
          pulse={connection.phase === 'connecting' || connection.phase === 'reconnecting'}
          size={6}
        />
        {connected ? <Wifi size={12} strokeWidth={1.7} /> : <WifiOff size={12} strokeWidth={1.7} />}
        <span>{PHASE_LABEL[connection.phase] ?? connection.phase}</span>
        <span className="mono text-fg-4">
          {connection.host}:{connection.port}
        </span>
        {connection.attempt > 0 && !connected ? (
          <span className="num text-fg-4">retry {connection.attempt}</span>
        ) : null}
      </StatusItem>

      <Separator />

      <StatusItem
        onClick={() => {
          if (canStopForge()) void stopForge()
          else if (canStartForge()) useUi.getState().setLauncherOpen(true)
        }}
        disabled={!canStartForge() && !canStopForge()}
        title={
          canStopForge()
            ? 'Stop the managed forge process'
            : canStartForge()
              ? 'Start forge.exe'
              : forge.message ?? 'No forge binary configured — set it in Settings → Forge'
        }
      >
        <Cpu size={12} strokeWidth={1.7} />
        <span>forge</span>
        {forge.phase === 'running' ? (
          <>
            <Square size={9} strokeWidth={2} className="text-st-running" />
            <span className="num text-fg-4">pid {forge.pid}</span>
          </>
        ) : (
          <>
            <Play size={9} strokeWidth={2} />
            <span className="text-fg-4">{forge.phase}</span>
          </>
        )}
      </StatusItem>

      {forge.phase === 'error' && forge.message ? (
        <>
          <Separator />
          <span
            className="truncate-1 flex max-w-[46ch] items-center gap-1.5 px-2 text-st-failed"
            title={forge.message}
          >
            <AlertTriangle size={12} strokeWidth={1.8} />
            {forge.message}
          </span>
        </>
      ) : null}

      <div className="ml-auto flex items-center gap-0.5">
        {scrubEventId !== null ? (
          <>
            <StatusItem onClick={() => setScrub(null)} title="Return to live state">
              <span className="text-st-waiting">● replaying @ {scrubEventId}</span>
              <span className="text-fg-4">click to resume</span>
            </StatusItem>
            <Separator />
          </>
        ) : null}

        {logsTruncated ? (
          <>
            <StatusItem
              onClick={() => setPanelTab('logs')}
              title="The log ring buffer has evicted older records"
            >
              <span className="text-fg-4">buffer trimmed</span>
            </StatusItem>
            <Separator />
          </>
        ) : null}

        {run ? (
          <span className="num flex items-center gap-2 px-2">
            <span title="Total nodes">{totals.total} nodes</span>
            {totals.running > 0 ? (
              <span className="text-st-running" title="Running">
                {totals.running} running
              </span>
            ) : null}
            {totals.waiting > 0 ? (
              <span className="text-st-waiting" title="Awaiting human approval">
                {totals.waiting} waiting
              </span>
            ) : null}
            {totals.failed > 0 ? (
              <span className="text-st-failed" title="Failed">
                {totals.failed} failed
              </span>
            ) : null}
          </span>
        ) : null}

        <Separator />
        <ThemeToggle />
        <Separator />
        <span className="num px-2 text-fg-4" title="Events received per second">
          {connection.eventRate} ev/s
        </span>
      </div>
    </footer>
  )
}

function ThemeToggle() {
  const theme = useStudio((s) => s.settings?.appearance.theme ?? 'dark')
  const Icon = theme === 'light' ? Sun : theme === 'dark' ? Moon : Monitor
  const next = nextTheme(theme)
  return (
    <StatusItem
      onClick={() => void bridge?.settings.patch({ appearance: { theme: next } })}
      title={`Theme: ${theme}. Click for ${next}. (Ctrl+Shift+L)`}
    >
      <Icon size={12} strokeWidth={1.7} />
    </StatusItem>
  )
}

function Separator() {
  return <span className="h-3 w-px shrink-0 bg-line-2" />
}

function StatusItem({
  children,
  onClick,
  title,
  disabled,
}: {
  children: React.ReactNode
  onClick?: () => void
  title?: string
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={title}
      className={cn(
        'flex h-full items-center gap-1.5 rounded-[3px] px-2 whitespace-nowrap',
        '[transition-property:background-color,color] duration-[var(--dur-fast)] ease-[var(--ease-out-quint)]',
        'focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-2',
        disabled
          ? 'cursor-default opacity-50'
          : 'hover:bg-bg-3 hover:text-fg-1 active:scale-[0.98]',
      )}
    >
      {children}
    </button>
  )
}
