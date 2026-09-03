import { useMemo, useState } from 'react'
import { Search } from 'lucide-react'
import { cn } from '@/design/cn'
import { EmptyState, Input, SectionLabel, StatusPip } from '@/design/primitives'
import {
  NODE_STATUS_LABEL,
  NODE_STATUS_VAR,
  durationVar,
  formatDuration,
} from '@/design/status'
import { useActiveRun, useStudio } from '@/state/store'
import type { NodeStatus } from '@shared/projection'

/** Failures and blocks first — the outline should surface what needs attention. */
const ORDER: NodeStatus[] = ['failed', 'waiting', 'running', 'pending', 'done']

export function GraphOutline() {
  const run = useActiveRun()
  const selectedNodeId = useStudio((s) => s.selectedNodeId)
  const selectNode = useStudio((s) => s.selectNode)
  const [search, setSearch] = useState('')

  const grouped = useMemo(() => {
    const groups = new Map<NodeStatus, typeof run extends undefined ? never : any[]>()
    if (!run) return groups
    const needle = search.trim().toLowerCase()
    for (const node of Object.values(run.nodes)) {
      if (
        needle &&
        !node.label.toLowerCase().includes(needle) &&
        !node.nodeId.toLowerCase().includes(needle)
      ) {
        continue
      }
      const list = groups.get(node.status) ?? []
      list.push(node)
      groups.set(node.status, list)
    }
    for (const list of groups.values()) {
      list.sort((a, b) => a.label.localeCompare(b.label))
    }
    return groups
  }, [run, search])

  if (!run) {
    return (
      <EmptyState
        title="No run selected"
        description="Pick a run from the Runs sidebar to see its nodes here."
      />
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="relative shrink-0 p-2">
        <Search
          size={12}
          strokeWidth={1.8}
          className="pointer-events-none absolute top-1/2 left-4 -translate-y-1/2 text-fg-4"
        />
        <Input
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder="Filter nodes"
          aria-label="Filter nodes"
          className="h-7 w-full pl-7 text-xs"
        />
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto pb-2">
        {ORDER.map((status) => {
          const nodes = grouped.get(status)
          if (!nodes || nodes.length === 0) return null
          return (
            <section key={status}>
              <SectionLabel>
                <span style={{ color: NODE_STATUS_VAR[status] }}>
                  {NODE_STATUS_LABEL[status]}
                </span>
                <span className="num ml-auto text-fg-4 normal-case">{nodes.length}</span>
              </SectionLabel>
              {nodes.map((node) => (
                <button
                  key={node.nodeId}
                  type="button"
                  onClick={() => selectNode(node.nodeId)}
                  className={cn(
                    'flex h-[var(--h-tree-row)] w-full items-center gap-2 px-3 text-left',
                    '[transition-property:background-color] duration-[var(--dur-fast)]',
                    selectedNodeId === node.nodeId
                      ? 'bg-accent-weak'
                      : 'hover:bg-bg-2',
                  )}
                >
                  <StatusPip
                    color={NODE_STATUS_VAR[node.status as NodeStatus]}
                    pulse={node.status === 'running'}
                    size={6}
                  />
                  <span className="truncate-1 min-w-0 flex-1 text-xs text-fg-2">
                    {node.label}
                  </span>
                  <span
                    className="num mono shrink-0 text-[10px]"
                    style={{ color: durationVar(node.durationMs) }}
                  >
                    {formatDuration(node.durationMs)}
                  </span>
                </button>
              ))}
            </section>
          )
        })}
      </div>
    </div>
  )
}
