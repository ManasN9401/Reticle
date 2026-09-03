import { CircleCheck, TriangleAlert } from 'lucide-react'
import { cn } from '@/design/cn'
import { EmptyState } from '@/design/primitives'
import { formatFailureReason, formatClock } from '@/design/status'
import { useActiveRun, useStudio } from '@/state/store'
import { useUi } from '@/state/ui'

/**
 * Failures across the active run, gathered into one list so a 60-node graph
 * does not have to be scanned by eye to find what broke.
 */
export function ProblemsPanel() {
  const run = useActiveRun()
  const selectNode = useStudio((s) => s.selectNode)
  const setView = useUi((s) => s.setView)

  const failures = run
    ? Object.values(run.nodes)
        .filter((node) => node.status === 'failed')
        .sort((a, b) => (b.endedAt ?? 0) - (a.endedAt ?? 0))
    : []

  if (!run) {
    return <EmptyState title="No active run" description="Problems appear here once a run starts." />
  }

  if (failures.length === 0) {
    return (
      <EmptyState
        icon={<CircleCheck size={20} strokeWidth={1.5} />}
        title="No failures"
        description={`All ${Object.keys(run.nodes).length} nodes in ${run.execId} are healthy.`}
      />
    )
  }

  return (
    <div className="h-full overflow-auto bg-inset">
      {failures.map((node) => (
        <button
          key={node.nodeId}
          type="button"
          onClick={() => {
            selectNode(node.nodeId)
            setView('graph')
          }}
          className={cn(
            'flex w-full items-start gap-2.5 border-b border-line-1 px-3 py-2 text-left',
            '[transition-property:background-color] duration-[var(--dur-fast)]',
            'hover:bg-bg-1 focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-2',
          )}
        >
          <TriangleAlert
            size={13}
            strokeWidth={1.8}
            className="mt-0.5 shrink-0 text-st-failed"
          />
          <div className="min-w-0 flex-1">
            <div className="flex items-baseline gap-2">
              <span className="text-xs font-medium text-fg-1">{node.label}</span>
              <span className="mono text-2xs text-fg-4">{node.nodeId}</span>
              {node.failure ? (
                <span className="text-2xs text-st-failed">
                  {formatFailureReason(node.failure.reason)}
                  {node.failure.exitCode !== undefined
                    ? ` · exit ${node.failure.exitCode}`
                    : ''}
                </span>
              ) : null}
              {node.endedAt ? (
                <span className="num mono ml-auto shrink-0 text-2xs text-fg-4">
                  {formatClock(node.endedAt)}
                </span>
              ) : null}
            </div>
            {node.failure?.stderr ? (
              <pre className="mono mt-1 max-h-20 overflow-hidden text-2xs whitespace-pre-wrap text-fg-3">
                {node.failure.stderr.split('\n').slice(0, 4).join('\n')}
              </pre>
            ) : null}
          </div>
        </button>
      ))}
    </div>
  )
}
