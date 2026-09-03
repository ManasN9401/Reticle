import {
  Boxes,
  FolderTree,
  ListTree,
  Package,
  Settings,
  Waypoints,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '@/design/cn'
import { pendingApprovals } from '@shared/projection'
import { useStudio } from '@/state/store'
import { useUi, type ViewId } from '@/state/ui'

interface RailItem {
  id: ViewId
  label: string
  icon: LucideIcon
  keys: string
}

const PRIMARY: RailItem[] = [
  { id: 'runs', label: 'Runs', icon: ListTree, keys: 'Ctrl+1' },
  { id: 'graph', label: 'Node Map', icon: Waypoints, keys: 'Ctrl+2' },
  { id: 'agents', label: 'Agents', icon: Boxes, keys: 'Ctrl+3' },
  { id: 'artifacts', label: 'Artifacts', icon: Package, keys: 'Ctrl+4' },
  { id: 'explorer', label: 'Explorer', icon: FolderTree, keys: 'Ctrl+5' },
]

const SETTINGS: RailItem = {
  id: 'settings',
  label: 'Settings',
  icon: Settings,
  keys: 'Ctrl+,',
}

export function ActivityRail() {
  const activeView = useUi((s) => s.activeView)
  const sidebarOpen = useUi((s) => s.sidebarOpen)
  const setView = useUi((s) => s.setView)
  const approvals = useStudio((s) => pendingApprovals(s.projection).length)

  return (
    <nav
      aria-label="Primary"
      className="flex w-[var(--h-rail)] shrink-0 flex-col items-center border-r border-line-1 bg-bg-1 py-1"
    >
      {PRIMARY.map((item) => (
        <RailButton
          key={item.id}
          item={item}
          active={activeView === item.id && sidebarOpen}
          badge={item.id === 'runs' && approvals > 0 ? approvals : undefined}
          onClick={() => setView(item.id)}
        />
      ))}
      <div className="mt-auto">
        <RailButton
          item={SETTINGS}
          active={activeView === SETTINGS.id && sidebarOpen}
          onClick={() => setView(SETTINGS.id)}
        />
      </div>
    </nav>
  )
}

function RailButton({
  item,
  active,
  badge,
  onClick,
}: {
  item: RailItem
  active: boolean
  badge?: number
  onClick: () => void
}) {
  const Icon = item.icon
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={item.label}
      aria-current={active ? 'page' : undefined}
      title={`${item.label}  ${item.keys}`}
      className={cn(
        'relative flex h-11 w-full items-center justify-center',
        '[transition-property:color,background-color] duration-[var(--dur-fast)] ease-[var(--ease-out-quint)]',
        'focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-2',
        active ? 'text-fg-1' : 'text-fg-4 hover:text-fg-2',
      )}
    >
      {/* Active indicator is a 2px rule, not a filled pill — chrome stays quiet. */}
      <span
        className={cn(
          'absolute top-1.5 bottom-1.5 left-0 w-0.5 rounded-r-full bg-accent',
          '[transition-property:opacity,transform] duration-[var(--dur-base)] ease-[var(--ease-out-quint)]',
          active ? 'scale-y-100 opacity-100' : 'scale-y-0 opacity-0',
        )}
      />
      <Icon size={18} strokeWidth={1.6} />
      {badge !== undefined ? (
        <span
          className="num absolute top-1.5 right-2 flex h-3.5 min-w-3.5 items-center justify-center rounded-full bg-st-waiting px-1 text-[9px] font-bold text-bg-0"
          title={`${badge} node${badge === 1 ? '' : 's'} awaiting approval`}
        >
          {badge}
        </span>
      ) : null}
    </button>
  )
}
