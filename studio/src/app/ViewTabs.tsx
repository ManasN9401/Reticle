import { ChevronRight, FileCode2, PanelRight, Waypoints, X } from 'lucide-react'
import { cn } from '@/design/cn'
import { IconButton } from '@/design/primitives'
import { useUi, type EditorTab } from '@/state/ui'

/**
 * Tab strip over the main surface. The node map is the home tab and cannot be
 * closed; file tabs open beside it when something is inspected.
 */
export function ViewTabs() {
  const tabs = useUi((s) => s.tabs)
  const activeTabId = useUi((s) => s.activeTabId)
  const setActiveTab = useUi((s) => s.setActiveTab)
  const closeTab = useUi((s) => s.closeTab)
  const inspectorOpen = useUi((s) => s.inspectorOpen)
  const toggleInspector = useUi((s) => s.toggleInspector)

  const active = tabs.find((t) => t.id === activeTabId)

  return (
    <div className="flex h-[var(--h-tabstrip)] shrink-0 items-stretch border-b border-line-1 bg-bg-1">
      <div className="flex min-w-0 flex-1 items-stretch overflow-x-auto">
        {tabs.map((tab) => (
          <Tab
            key={tab.id}
            tab={tab}
            active={tab.id === activeTabId}
            closable={tab.kind !== 'graph'}
            onSelect={() => setActiveTab(tab.id)}
            onClose={() => closeTab(tab.id)}
          />
        ))}
      </div>

      {active?.subtitle ? (
        <div className="hidden min-w-0 items-center gap-1 px-3 text-xs text-fg-4 lg:flex">
          <Breadcrumb path={active.subtitle} />
        </div>
      ) : null}

      <div className="flex shrink-0 items-center border-l border-line-1 px-1">
        <IconButton
          label={inspectorOpen ? 'Hide inspector' : 'Show inspector'}
          size="sm"
          active={inspectorOpen}
          onClick={toggleInspector}
        >
          <PanelRight size={14} strokeWidth={1.7} />
        </IconButton>
      </div>
    </div>
  )
}

function Tab({
  tab,
  active,
  closable,
  onSelect,
  onClose,
}: {
  tab: EditorTab
  active: boolean
  closable: boolean
  onSelect: () => void
  onClose: () => void
}) {
  const Icon = tab.kind === 'graph' ? Waypoints : FileCode2

  return (
    <div
      role="tab"
      tabIndex={0}
      aria-selected={active}
      onClick={onSelect}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          onSelect()
        }
      }}
      onAuxClick={(event) => {
        // Middle-click closes, as in every editor.
        if (event.button === 1 && closable) {
          event.preventDefault()
          onClose()
        }
      }}
      className={cn(
        'group relative flex max-w-[220px] min-w-0 cursor-pointer items-center gap-2 border-r border-line-1 pr-2 pl-3 select-none',
        '[transition-property:background-color,color] duration-[var(--dur-fast)] ease-[var(--ease-out-quint)]',
        'focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-2',
        active ? 'bg-bg-0 text-fg-1' : 'bg-bg-1 text-fg-3 hover:bg-bg-2 hover:text-fg-2',
      )}
    >
      {/* Active tab is marked by a top rule, so tab width never changes. */}
      <span
        className={cn(
          'absolute inset-x-0 top-0 h-0.5 bg-accent',
          '[transition-property:opacity] duration-[var(--dur-base)]',
          active ? 'opacity-100' : 'opacity-0',
        )}
      />
      <Icon size={13} strokeWidth={1.7} className="shrink-0" />
      <span className="truncate-1 text-xs">{tab.title}</span>
      {closable ? (
        <button
          type="button"
          aria-label={`Close ${tab.title}`}
          onClick={(event) => {
            event.stopPropagation()
            onClose()
          }}
          className={cn(
            'flex h-4 w-4 shrink-0 items-center justify-center rounded-[3px] text-fg-4',
            'opacity-0 group-hover:opacity-100 hover:bg-bg-3 hover:text-fg-1 focus-visible:opacity-100',
            '[transition-property:opacity,background-color,color] duration-[var(--dur-fast)]',
          )}
        >
          <X size={11} strokeWidth={2} />
        </button>
      ) : (
        <span className="w-4 shrink-0" />
      )}
    </div>
  )
}

function Breadcrumb({ path }: { path: string }) {
  const parts = path.split(/[\\/]/).filter(Boolean)
  const shown = parts.slice(-4)
  return (
    <div className="mono flex min-w-0 items-center gap-1 text-2xs">
      {parts.length > shown.length ? <span className="text-fg-4">…</span> : null}
      {shown.map((part, index) => (
        <span key={`${part}-${index}`} className="flex min-w-0 items-center gap-1">
          {index > 0 ? (
            <ChevronRight size={10} strokeWidth={2} className="shrink-0 text-fg-4" />
          ) : null}
          <span
            className={cn('truncate-1', index === shown.length - 1 && 'text-fg-2')}
          >
            {part}
          </span>
        </span>
      ))}
    </div>
  )
}
