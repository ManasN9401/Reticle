import { Handle, Position, type NodeProps } from '@xyflow/react'
import { FlaskConical, Package, RotateCw, TriangleAlert } from 'lucide-react'
import { memo } from 'react'
import { cn } from '@/design/cn'
import {
  NODE_STATUS_LABEL,
  NODE_STATUS_VAR,
  durationVar,
  formatDuration,
  formatFailureReason,
} from '@/design/status'
import { useUi } from '@/state/ui'
import type { AgentFlowNode } from './layout'
import { NODE_HEIGHT, NODE_WIDTH } from './layout'

/**
 * The DAG node.
 *
 * Replaces React Flow's default rectangle. Everything an operator needs to
 * triage a run at a glance is on the face of the node — status, who ran, which
 * model, how long, how many retries, what it produced — and the status rail on
 * the left is readable at zoom levels where the text is not.
 */
export const AgentNode = memo(function AgentNode({
  data,
  selected,
}: NodeProps<AgentFlowNode>) {
  const { node, blamed } = data
  const setReviewNode = useUi((s) => s.setReviewNode)

  const statusColor = NODE_STATUS_VAR[node.status]
  const running = node.status === 'running'
  const failed = node.status === 'failed'
  const waitingOnHuman = node.status === 'waiting' && node.waiting?.kind === 'human'

  return (
    <div
      className={cn(
        'relative flex overflow-hidden rounded-[var(--radius-node)] border bg-bg-2 select-none',
        '[transition-property:border-color,box-shadow,background-color] duration-[var(--dur-fast)] ease-[var(--ease-out-quint)]',
        running && 'node-sweep',
        selected
          ? 'border-accent shadow-[0_0_0_1px_var(--color-accent)]'
          : failed
            ? 'border-st-failed/45'
            : blamed
              ? 'border-st-failed/30'
              : 'border-line-2 hover:border-line-3',
      )}
      style={{ width: NODE_WIDTH, height: NODE_HEIGHT }}
    >
      <Handle type="target" position={Position.Top} />

      {/* Status rail: the thing you actually read when zoomed out. */}
      <span
        aria-hidden
        className={cn(
          'w-[3px] shrink-0 rounded-l-[7px]',
          running && 'node-rail-pulse',
        )}
        style={{ backgroundColor: statusColor }}
      />

      <div className="flex min-w-0 flex-1 flex-col justify-center gap-0.5 px-2.5 py-1.5">
        <div className="flex items-center gap-1.5">
          <span
            className="h-1.5 w-1.5 shrink-0 rounded-full"
            style={{ backgroundColor: statusColor }}
            title={NODE_STATUS_LABEL[node.status]}
          />
          {/* RFC-022 law 5: show the semantic agent, not the generic node id. */}
          <span
            className="truncate-1 min-w-0 flex-1 text-xs font-medium text-fg-1"
            title={node.agentId ?? node.nodeId}
          >
            {node.label}
          </span>
          <span
            className="num mono shrink-0 text-2xs"
            style={{ color: durationVar(node.durationMs) }}
            title={node.durationMs === undefined ? 'No duration yet' : 'Derived duration'}
          >
            {node.durationMs === undefined && running
              ? '···'
              : formatDuration(node.durationMs)}
          </span>
        </div>

        <div className="mono truncate-1 text-2xs text-fg-4" title={node.nodeId}>
          {node.nodeId}
        </div>

        <div className="flex min-w-0 items-center gap-1.5">
          {node.model ? (
            <span
              className="mono truncate-1 min-w-0 rounded-[3px] border border-line-2 px-1 text-[10px] text-fg-3"
              title={node.model}
            >
              {shortModel(node.model)}
            </span>
          ) : (
            <span className="text-[10px] text-fg-4">—</span>
          )}

          <span className="ml-auto flex shrink-0 items-center gap-1.5">
            {node.artifacts.length > 0 ? (
              <span
                className="num flex items-center gap-0.5 text-[10px] text-fg-3"
                title={`${node.artifacts.length} artifact${node.artifacts.length === 1 ? '' : 's'} produced`}
              >
                <Package size={9} strokeWidth={2} />
                {node.artifacts.length}
              </span>
            ) : null}

            {node.attempts > 1 ? (
              <span
                className="num flex items-center gap-0.5 text-[10px] text-st-waiting"
                title={`${node.attempts} attempts — retried ${node.attempts - 1} time${node.attempts === 2 ? '' : 's'}`}
              >
                <RotateCw size={9} strokeWidth={2} />
                {node.attempts - 1}
              </span>
            ) : null}

            {/* RFC-026 §8: a mock fallback must never look like real work. */}
            {node.mocked ? (
              <span
                className="flex items-center text-st-waiting"
                title="This node fell back to mock output — the result is not real work"
              >
                <FlaskConical size={10} strokeWidth={2} />
              </span>
            ) : null}

            {failed && node.failure ? (
              <span
                className="flex items-center text-st-failed"
                title={`${formatFailureReason(node.failure.reason)}${
                  node.failure.exitCode !== undefined
                    ? ` (exit ${node.failure.exitCode})`
                    : ''
                }`}
              >
                <TriangleAlert size={10} strokeWidth={2} />
              </span>
            ) : null}
          </span>
        </div>
      </div>

      {/* A blocked node gets a real, working control — not a status badge. */}
      {waitingOnHuman ? (
        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation()
            setReviewNode(node.nodeId)
          }}
          className={cn(
            'absolute right-1.5 bottom-1.5 flex h-[18px] items-center rounded-[3px] px-1.5 text-[10px] font-semibold tracking-wide uppercase',
            'bg-st-waiting text-bg-0',
            '[transition-property:filter,transform] duration-[var(--dur-fast)]',
            'hover:brightness-110 active:scale-[0.95]',
          )}
          title="Review and approve or reject this checkpoint"
        >
          Review
        </button>
      ) : null}

      <Handle type="source" position={Position.Bottom} />
    </div>
  )
})

/** `groq/llama-3.1-8b-instant` reads better as `llama-3.1-8b-instant` on a chip. */
function shortModel(model: string): string {
  const slash = model.indexOf('/')
  return slash === -1 ? model : model.slice(slash + 1)
}
