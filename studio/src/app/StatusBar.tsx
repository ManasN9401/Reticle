import { useEffect, useRef, useState } from 'react'
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
import { Button, Input, StatusPip } from '@/design/primitives'
import { connectionVar } from '@/design/status'
import {
  canStartForge,
  canStopForge,
  connectTo,
  disconnect,
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


  return (
    <footer className="flex h-[var(--h-statusbar)] shrink-0 items-center gap-0.5 border-t border-line-1 bg-bg-1 px-1 text-xs text-fg-3">
      <ConnectionControl />

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

/**
 * The connection chip. Clicking it opens a small form to choose the address to
 * connect to, because Forge can be launched on any port and the saved default is
 * not always the one that is listening.
 */
function ConnectionControl() {
  const connection = useStudio((s) => s.connection)
  const saved = useStudio((s) => s.settings?.connection)
  const connected = connection.phase === 'connected'
  const [open, setOpen] = useState(false)
  const [host, setHost] = useState(connection.host)
  const [port, setPort] = useState(String(connection.port))
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onPointer = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const toggle = () => {
    if (!open) {
      setHost(saved?.host ?? connection.host)
      setPort(String(connection.port))
    }
    setOpen((value) => !value)
  }

  const portNumber = Number(port)
  const valid = Number.isInteger(portNumber) && portNumber >= 1 && portNumber <= 65535 && host.trim() !== ''

  return (
    <div ref={rootRef} className="relative h-full">
      <StatusItem onClick={toggle} title="Choose the orchestrator address to connect to">
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

      {open ? (
        <form
          aria-label="Connect to orchestrator"
          className="absolute bottom-full left-0 z-50 mb-1 flex w-72 flex-col gap-2 rounded-[var(--radius-card)] border border-line-2 bg-bg-2 p-3 shadow-[var(--shadow-modal)]"
          onSubmit={(event) => {
            event.preventDefault()
            if (!valid) return
            void connectTo(host.trim(), portNumber)
            setOpen(false)
          }}
        >
          <div className="text-xs font-medium text-fg-1">Connect to orchestrator</div>
          <div className="flex gap-2">
            <label className="flex min-w-0 flex-1 flex-col gap-1 text-2xs text-fg-3">
              Host
              <Input value={host} onChange={(event) => setHost(event.target.value)} className="h-7 text-xs" />
            </label>
            <label className="flex w-20 flex-col gap-1 text-2xs text-fg-3">
              Port
              <Input
                value={port}
                inputMode="numeric"
                onChange={(event) => setPort(event.target.value)}
                className="h-7 text-xs"
                aria-invalid={!valid}
              />
            </label>
          </div>
          <p className="pretty text-2xs text-fg-4">
            The address is saved as your default. Forge launched from Studio on another port is connected to automatically.
          </p>
          <div className="flex justify-end gap-1.5">
            {connected ? (
              <Button size="sm" variant="subtle" onClick={() => { void disconnect(); setOpen(false) }}>
                Disconnect
              </Button>
            ) : null}
            <Button size="sm" variant="primary" type="submit" disabled={!valid}>
              {connected ? 'Reconnect' : 'Connect'}
            </Button>
          </div>
        </form>
      ) : null}
    </div>
  )
}
