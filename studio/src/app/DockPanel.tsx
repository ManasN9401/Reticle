import { ChevronDown, Globe, Maximize2, Minimize2, SquareTerminal } from 'lucide-react'
import { cn } from '@/design/cn'
import { EmptyState, IconButton } from '@/design/primitives'
import { LogPanel } from '@/features/logs/LogPanel'
import { ProblemsPanel } from '@/features/problems/ProblemsPanel'
import { useActiveRun, useStudio } from '@/state/store'
import { useUi, type PanelTab } from '@/state/ui'

const TABS: { id: PanelTab; label: string }[] = [
  { id: 'logs', label: 'Logs' },
  { id: 'problems', label: 'Problems' },
  { id: 'terminal', label: 'Terminal' },
  { id: 'preview', label: 'Preview' },
]

export function DockPanel() {
  const panelTab = useUi((s) => s.panelTab)
  const setPanelTab = useUi((s) => s.setPanelTab)
  const togglePanel = useUi((s) => s.togglePanel)
  const maximized = useUi((s) => s.panelMaximized)
  const toggleMaximized = useUi((s) => s.togglePanelMaximized)

  const run = useActiveRun()
  const failureCount = run
    ? Object.values(run.nodes).filter((n) => n.status === 'failed').length
    : 0
  const logCount = useStudio((s) => s.logs.length)

  return (
    <section
      aria-label="Panel"
      className="flex min-h-0 flex-1 flex-col border-t border-line-1 bg-bg-0"
    >
      <div className="flex h-[var(--h-tabstrip)] shrink-0 items-stretch bg-bg-1">
        {TABS.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={panelTab === tab.id}
            onClick={() => setPanelTab(tab.id)}
            className={cn(
              'relative flex items-center gap-1.5 px-3 text-xs whitespace-nowrap',
              '[transition-property:color,background-color] duration-[var(--dur-fast)] ease-[var(--ease-out-quint)]',
              'focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-2',
              panelTab === tab.id ? 'text-fg-1' : 'text-fg-3 hover:text-fg-2',
            )}
          >
            <span
              className={cn(
                'absolute inset-x-2 bottom-0 h-0.5 rounded-t-full bg-accent',
                '[transition-property:opacity] duration-[var(--dur-base)]',
                panelTab === tab.id ? 'opacity-100' : 'opacity-0',
              )}
            />
            {tab.label}
            {tab.id === 'problems' && failureCount > 0 ? (
              <span className="num rounded-full bg-st-failed-weak px-1.5 text-2xs text-st-failed">
                {failureCount}
              </span>
            ) : null}
            {tab.id === 'logs' && logCount > 0 ? (
              <span className="num text-2xs text-fg-4">{compact(logCount)}</span>
            ) : null}
          </button>
        ))}

        <div className="ml-auto flex items-center gap-0.5 px-1">
          <IconButton
            label={maximized ? 'Restore panel' : 'Maximize panel'}
            size="sm"
            onClick={toggleMaximized}
          >
            {maximized ? (
              <Minimize2 size={13} strokeWidth={1.7} />
            ) : (
              <Maximize2 size={13} strokeWidth={1.7} />
            )}
          </IconButton>
          <IconButton label="Hide panel" size="sm" onClick={togglePanel}>
            <ChevronDown size={14} strokeWidth={1.8} />
          </IconButton>
        </div>
      </div>

      <div className="min-h-0 flex-1">
        {panelTab === 'logs' ? <LogPanel /> : null}
        {panelTab === 'problems' ? <ProblemsPanel /> : null}
        {panelTab === 'terminal' ? <TerminalPlaceholder /> : null}
        {panelTab === 'preview' ? <PreviewPlaceholder /> : null}
      </div>
    </section>
  )
}

/**
 * Not-yet-built surfaces say what they will be rather than presenting a
 * disabled control with no explanation. An honest empty state is better than a
 * button that does nothing.
 */
function TerminalPlaceholder() {
  return (
    <div className="h-full bg-inset">
      <EmptyState
        icon={<SquareTerminal size={22} strokeWidth={1.4} />}
        title="Terminal is not wired up yet"
        description={
          <>
            This panel will host an interactive shell in the workspace directory, backed by
            a pty. For now, forge's own stdout and stderr are folded into the{' '}
            <span className="text-fg-2">Logs</span> tab.
          </>
        }
      />
    </div>
  )
}

function PreviewPlaceholder() {
  return (
    <div className="h-full bg-inset">
      <EmptyState
        icon={<Globe size={22} strokeWidth={1.4} />}
        title="Preview is not wired up yet"
        description={
          <>
            This panel will render a browser view of whatever a run serves, outside the
            agent sandbox. Generated files can be opened from{' '}
            <span className="text-fg-2">Artifacts</span> in the meantime.
          </>
        }
      />
    </div>
  )
}

function compact(value: number): string {
  if (value < 1000) return String(value)
  if (value < 1_000_000) return `${(value / 1000).toFixed(value < 10_000 ? 1 : 0)}k`
  return `${(value / 1_000_000).toFixed(1)}M`
}
