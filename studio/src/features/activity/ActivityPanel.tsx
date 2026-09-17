import { useMemo, useState } from 'react'
import {
  Activity,
  CircleEllipsis,
  PackageCheck,
  PackageOpen,
  TerminalSquare,
  TriangleAlert,
} from 'lucide-react'
import { cn } from '@/design/cn'
import { Badge, Chip, EmptyState, StatusPip } from '@/design/primitives'
import { formatClock, formatDuration, NODE_STATUS_LABEL, NODE_STATUS_VAR } from '@/design/status'
import { compactToolLog } from '@/features/graph/toolLog'
import { useActiveRun, useStudio } from '@/state/store'
import type { LogRecord } from '@shared/ipc'
import { buildDependencyOperations, type DependencyOperation } from './dependencyActivity'

type ActivityTab = 'progress' | 'dependencies' | 'tools'

const DEPENDENCY_MESSAGE = /(?:installing (?:base|agent) dependencies|(?:base|agent) dependencies ready|(?:base|agent) dependency installation failed)/i

export function ActivityPanel() {
  const run = useActiveRun()
  const logs = useStudio((state) => state.logs)
  const selectedNodeId = useStudio((state) => state.selectedNodeId)
  const [tab, setTab] = useState<ActivityTab>('progress')

  const dependencies = useMemo(() => buildDependencyOperations(logs), [logs])
  const tools = useMemo(
    () => logs.filter((record) => !record.isLlm && /\[TOOL\]/i.test(record.message)),
    [logs],
  )
  const activity = useMemo(
    () => logs.filter((record) => !record.isLlm && (record.eventType || DEPENDENCY_MESSAGE.test(record.message))).slice(-80).reverse(),
    [logs],
  )

  const tabs: { id: ActivityTab; label: string; count?: number }[] = [
    { id: 'progress', label: 'Progress' },
    { id: 'dependencies', label: 'Dependencies', count: dependencies.length },
    { id: 'tools', label: 'Tools', count: tools.length },
  ]

  return (
    <div className="flex h-full min-h-0 flex-col bg-inset">
      <div className="flex h-8 shrink-0 items-stretch border-b border-line-1 bg-bg-1 px-1">
        {tabs.map((item) => (
          <button
            key={item.id}
            type="button"
            onClick={() => setTab(item.id)}
            className={cn('relative flex items-center gap-1.5 px-3 text-xs','focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-2',tab === item.id ? 'text-fg-1' : 'text-fg-3 hover:text-fg-2')}
          >
            <span className={cn('absolute inset-x-2 bottom-0 h-0.5 rounded-t-full bg-accent',tab === item.id ? 'opacity-100' : 'opacity-0')} />
            {item.label}
            {item.count ? <span className="num text-2xs text-fg-4">{item.count}</span> : null}
          </button>
        ))}
        <div className="mono ml-auto flex items-center px-3 text-2xs text-fg-4">
          {selectedNodeId ? `node: ${selectedNodeId}` : run?.execId ?? 'no active run'}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {tab === 'progress' ? <ProgressView records={activity} /> : null}
        {tab === 'dependencies' ? <DependenciesView operations={dependencies} /> : null}
        {tab === 'tools' ? <ToolsView records={tools} /> : null}
      </div>
    </div>
  )
}

function ProgressView({ records }: { records: LogRecord[] }) {
  const run = useActiveRun()
  if (!run) {
    return <EmptyState icon={<Activity size={20} />} title="No active run" description="Start or select a run to see its live work." />
  }
  const nodes = Object.values(run.nodes)
  const active = nodes.filter((node) => node.status === 'running' || node.status === 'waiting')
  const completed = nodes.filter((node) => node.status === 'done').length
  const failed = nodes.filter((node) => node.status === 'failed').length

  return (
    <div className="grid min-h-full grid-cols-[minmax(240px,0.8fr)_minmax(360px,1.2fr)] divide-x divide-line-1">
      <div className="min-w-0 p-3">
        <div className="mb-2 flex items-center gap-2">
          <StatusPip color={run.status === 'failed' ? 'var(--color-st-failed)' : run.status === 'completed' ? 'var(--color-st-done)' : 'var(--color-st-running)'} pulse={run.status === 'running'} />
          <span className="text-sm font-medium text-fg-1">{run.workflowId ?? run.execId}</span>
          <Badge className="ml-auto">{run.status}</Badge>
        </div>
        <div className="grid grid-cols-3 gap-1.5">
          <RunMetric label="Complete" value={`${completed}/${nodes.length}`} />
          <RunMetric label="Active" value={String(active.length)} />
          <RunMetric label="Failed" value={String(failed)} failed={failed > 0} />
        </div>
        <div className="mt-3 text-2xs font-semibold tracking-wide text-fg-3 uppercase">Working now</div>
        {active.length === 0 ? (
          <div className="mt-2 text-xs text-fg-4">No node is currently running.</div>
        ) : (
          <div className="mt-1.5 flex flex-col gap-1.5">
            {active.map((node) => (
              <div key={node.nodeId} className="rounded-[var(--radius-control)] border border-line-1 bg-bg-1 px-2.5 py-2">
                <div className="flex items-center gap-2">
                  <StatusPip color={NODE_STATUS_VAR[node.status]} pulse={node.status === 'running'} />
                  <span className="truncate-1 min-w-0 flex-1 text-xs font-medium text-fg-2">{node.label}</span>
                  <span className="text-2xs text-fg-4">{NODE_STATUS_LABEL[node.status]}</span>
                </div>
                <div className="mt-1 flex gap-3 text-2xs text-fg-4">
                  <span>{node.model ?? 'model pending'}</span>
                  <span>{formatDuration(node.durationMs)}</span>
                  <span>attempt {Math.max(1, node.attempts)}</span>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="min-w-0">
        <div className="border-b border-line-1 px-3 py-2 text-2xs font-semibold tracking-wide text-fg-3 uppercase">Recent milestones</div>
        {records.length === 0 ? (
          <EmptyState title="No activity reported yet" description="Lifecycle and setup milestones will appear here." />
        ) : (
          records.map((record) => <ActivityRow key={record.seq} record={record} />)
        )}
      </div>
    </div>
  )
}

function RunMetric({ label, value, failed }: { label: string; value: string; failed?: boolean }) {
  return (
    <div className="rounded-[var(--radius-control)] border border-line-1 bg-bg-1 px-2 py-1.5">
      <div className="text-[10px] text-fg-4">{label}</div>
      <div className={cn('num mt-0.5 text-sm', failed ? 'text-st-failed' : 'text-fg-2')}>{value}</div>
    </div>
  )
}

function ActivityRow({ record }: { record: LogRecord }) {
  return (
    <div className="flex items-start gap-2 border-b border-line-1 px-3 py-1.5">
      <span className="num mt-px shrink-0 text-[10px] text-fg-4">{formatClock(record.at)}</span>
      <StatusPip className="mt-1" size={6} color={record.level === 'error' ? 'var(--color-st-failed)' : record.level === 'warn' ? 'var(--color-st-waiting)' : 'var(--color-fg-4)'} />
      <div className="min-w-0 flex-1">
        <div className={cn('text-xs', record.level === 'error' ? 'text-st-failed' : 'text-fg-2')}>{compactToolLog(record.message)}</div>
        {record.agentId ? <div className="mono mt-0.5 text-[10px] text-fg-4">{record.agentId}</div> : null}
      </div>
    </div>
  )
}

function DependenciesView({ operations }: { operations: DependencyOperation[] }) {
  if (operations.length === 0) {
    return <EmptyState icon={<PackageOpen size={20} />} title="No dependency activity" description="Environment creation, cached package sets, installs and failures will be shown here." />
  }
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(300px,1fr))] gap-2 p-3">
      {operations.map((operation) => {
        const Icon = operation.status === 'ready' ? PackageCheck : operation.status === 'failed' ? TriangleAlert : CircleEllipsis
        const color = operation.status === 'ready' ? 'text-st-done' : operation.status === 'failed' ? 'text-st-failed' : 'text-st-running'
        return (
          <div key={operation.key} className="rounded-[var(--radius-control)] border border-line-1 bg-bg-1 p-3">
            <div className="flex items-center gap-2">
              <Icon size={14} className={cn(color, operation.status === 'installing' && 'animate-pulse')} />
              <span className="truncate-1 min-w-0 flex-1 text-xs font-medium text-fg-1">{operation.agent ?? 'Shared base environment'}</span>
              <Badge>{operation.cached ? 'cached' : operation.status}</Badge>
            </div>
            <div className="mt-1.5 flex items-center gap-3 text-2xs text-fg-4">
              <span>{operation.manager}</span>
              <span>{operation.dependencies.length} packages</span>
              {operation.durationMs !== undefined ? <span>{formatDuration(operation.durationMs)}</span> : null}
              <span className="num ml-auto">{formatClock(operation.updatedAt)}</span>
            </div>
            <div className="mt-2 flex flex-wrap gap-1">
              {operation.dependencies.map((dependency) => <Chip key={dependency}>{dependency}</Chip>)}
            </div>
          </div>
        )
      })}
    </div>
  )
}

function ToolsView({ records }: { records: LogRecord[] }) {
  if (records.length === 0) {
    return <EmptyState icon={<TerminalSquare size={20} />} title="No tool activity" description="File, terminal, browser and other worker tool calls will appear here." />
  }
  return <div>{records.slice().reverse().map((record) => <ActivityRow key={record.seq} record={record} />)}</div>
}
