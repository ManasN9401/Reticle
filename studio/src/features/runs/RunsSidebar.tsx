import { KeyRound, Trash2, Pause, Play, XOctagon } from 'lucide-react'
import { cn } from '@/design/cn'
import { EmptyState, SectionLabel, StatusPip, Tooltip } from '@/design/primitives'
import {
  executionStatusVar,
  formatDuration,
  formatRelative,
  runStatusVar,
} from '@/design/status'
import { removeFromQueue, killRun, pauseRun, resumeRun } from '@/state/actions'
import { useStudio } from '@/state/store'
import { pendingApprovals, runTotals } from '@shared/projection'
import { useUi } from '@/state/ui'
import { useNow } from '@/state/useNow'
import { Composer } from './Composer'

/**
 * Runs sidebar: pending approvals first (they block the graph), then the live
 * queue, then every run Studio has observed, with the composer pinned below.
 */
export function RunsSidebar() {
  const projection = useStudio((s) => s.projection)
  const selectedExecId = useStudio((s) => s.selectedExecId)
  const selectRun = useStudio((s) => s.selectRun)
  const selectNode = useStudio((s) => s.selectNode)
  const setReviewNode = useUi((s) => s.setReviewNode)
  const setView = useUi((s) => s.setView)

  const waitlist = projection.waitlist
  const approvals = pendingApprovals(projection)
  const runs = [...projection.runOrder].reverse()
  const queued = waitlist?.items ?? []
  const anyRunning = runs.some((id) => projection.runs[id]?.status === 'running')
  const now = useNow(anyRunning)

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="min-h-0 flex-1 overflow-y-auto">
        {approvals.length > 0 ? (
          <section>
            <SectionLabel className="text-st-waiting">
              Awaiting approval · {approvals.length}
            </SectionLabel>
            {approvals.map((node) => (
              <button
                key={node.taskId}
                type="button"
                onClick={() => {
                  selectRun(node.execId)
                  selectNode(node.nodeId)
                  setView('graph')
                  setReviewNode(node.nodeId)
                }}
                className="flex w-full items-center gap-2 border-l-2 border-st-waiting bg-st-waiting-weak px-3 py-1.5 text-left hover:brightness-125"
              >
                <div className="min-w-0 flex-1">
                  <div className="truncate-1 text-xs text-fg-1">{node.label}</div>
                  <div className="mono truncate-1 text-2xs text-fg-4">{node.execId}</div>
                </div>
                <span className="shrink-0 text-2xs font-semibold text-st-waiting uppercase">
                  Review
                </span>
              </button>
            ))}
          </section>
        ) : null}

        <section>
          <SectionLabel>
            Queue
            {waitlist ? (
              <span className="num ml-auto text-fg-4 normal-case">
                {waitlist.runningWorkers}/{waitlist.maxWorkers} workers
              </span>
            ) : null}
          </SectionLabel>

          {queued.length === 0 ? (
            <p className="px-3 py-2 text-2xs text-fg-4">
              Nothing queued. Submit a prompt below.
            </p>
          ) : (
            queued.map((item) => (
              <div
                key={item.id}
                className={cn(
                  'group flex w-full items-start gap-2 px-3 py-1.5',
                  '[transition-property:background-color] duration-[var(--dur-fast)]',
                  selectedExecId === item.id ? 'bg-accent-weak' : 'hover:bg-bg-2',
                )}
              >
                <button
                  type="button"
                  onClick={() => selectRun(item.id)}
                  className="flex min-w-0 flex-1 items-start gap-2 text-left"
                >
                  <StatusPip
                    className="mt-1"
                    color={executionStatusVar(item.status)}
                    pulse={item.status === 'RUNNING'}
                  />
                  <span className="min-w-0 flex-1">
                    <span className="mono flex items-baseline gap-1.5 text-2xs text-fg-4">
                      {item.id}
                      {item.effort && item.effort !== 'auto' ? (
                        <span>· {item.effort}</span>
                      ) : null}
                      <span className="ml-auto">
                        {formatRelative(
                          item.created_at ? Date.parse(item.created_at) : undefined,
                        )}
                      </span>
                    </span>
                    <span className="pretty mt-0.5 block max-h-8 overflow-hidden text-xs text-fg-2">
                      {item.prompt}
                    </span>
                  </span>
                </button>
                <div className="mt-0.5 flex items-center gap-1 opacity-0 group-hover:opacity-100 focus-within:opacity-100">
                  {item.status === 'RUNNING' && (
                    <Tooltip content="Pause run">
                      <button
                        type="button"
                        onClick={() => pauseRun(item.id)}
                        className="flex h-5 w-5 shrink-0 items-center justify-center rounded-[3px] text-fg-4 hover:bg-bg-3 hover:text-fg-1"
                      >
                        <Pause size={11} strokeWidth={2} />
                      </button>
                    </Tooltip>
                  )}
                  {item.status === 'PAUSED' && (
                    <Tooltip content="Resume run">
                      <button
                        type="button"
                        onClick={() => resumeRun(item.id)}
                        className="flex h-5 w-5 shrink-0 items-center justify-center rounded-[3px] text-fg-4 hover:bg-bg-3 hover:text-fg-1"
                      >
                        <Play size={11} strokeWidth={2} />
                      </button>
                    </Tooltip>
                  )}
                  {(item.status === 'RUNNING' || item.status === 'PAUSED') && (
                    <Tooltip content="Kill run">
                      <button
                        type="button"
                        onClick={() => killRun(item.id)}
                        className="flex h-5 w-5 shrink-0 items-center justify-center rounded-[3px] text-fg-4 hover:bg-bg-3 hover:text-st-failed"
                      >
                        <XOctagon size={11} strokeWidth={1.9} />
                      </button>
                    </Tooltip>
                  )}
                  {item.status === 'PENDING' && (
                    <Tooltip content="Remove from queue">
                      <button
                        type="button"
                        onClick={() => removeFromQueue(item.id)}
                        className="flex h-5 w-5 shrink-0 items-center justify-center rounded-[3px] text-fg-4 hover:bg-bg-3 hover:text-st-failed"
                      >
                        <Trash2 size={11} strokeWidth={1.9} />
                      </button>
                    </Tooltip>
                  )}
                </div>
              </div>
            ))
          )}
        </section>

        <section>
          <SectionLabel>Observed runs</SectionLabel>
          {runs.length === 0 ? (
            <EmptyState
              className="py-6"
              title="No runs yet"
              description="Runs appear here as the orchestrator emits workflow events."
            />
          ) : (
            runs.map((execId) => {
              const run = projection.runs[execId]
              if (!run) return null
              const totals = runTotals(run)
              const elapsed =
                run.startedAt !== undefined
                  ? formatDuration((run.endedAt ?? now) - run.startedAt)
                  : undefined
              return (
                <button
                  key={execId}
                  type="button"
                  onClick={() => selectRun(execId)}
                  className={cn(
                    'flex w-full items-center gap-2 px-3 py-1.5 text-left',
                    '[transition-property:background-color] duration-[var(--dur-fast)]',
                    selectedExecId === execId ? 'bg-accent-weak' : 'hover:bg-bg-2',
                  )}
                >
                  <StatusPip
                    color={runStatusVar(run.status)}
                    pulse={run.status === 'running'}
                  />
                  <span className="mono truncate-1 min-w-0 flex-1 text-xs text-fg-2">
                    {execId}
                  </span>
                  <span className="num shrink-0 text-2xs text-fg-4">
                    {totals.done}/{totals.total}
                  </span>
                  {totals.failed > 0 ? (
                    <span className="num shrink-0 text-2xs text-st-failed">
                      {totals.failed}
                    </span>
                  ) : null}
                  {elapsed ? (
                    <span className="num mono shrink-0 text-2xs text-fg-4">{elapsed}</span>
                  ) : null}
                </button>
              )
            })
          )}
        </section>

        {waitlist?.lockedKeys && waitlist.lockedKeys.length > 0 ? (
          <section>
            <SectionLabel>Unavailable keys · {waitlist.lockedKeys.length}</SectionLabel>
            <div className="flex flex-wrap gap-1 px-3 pb-2">
              {waitlist.lockedKeys.map((key) => (
                <span
                  key={key}
                  className="mono flex items-center gap-1 rounded-[3px] border border-line-2 bg-bg-2 px-1.5 py-0.5 text-[10px] text-st-waiting"
                  title="This API key failed discovery or exhausted its reported limit"
                >
                  <KeyRound size={9} strokeWidth={2} />
                  {key}
                </span>
              ))}
            </div>
          </section>
        ) : null}
      </div>

      <Composer />
    </div>
  )
}
