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
import { HEX_LABEL_HEIGHT, NODE_GEOMETRY, hexPoints } from './layout'
import type { RunNode } from '@shared/projection'

/**
 * The DAG node, in three presentations.
 *
 * All three encode status as a saturated colour on an otherwise achromatic
 * shape, so the state of a run is readable at zoom levels where no text is.
 * They differ only in how much supporting detail they carry:
 *
 *  - detailed: the full card — who ran, on what model, how long, what it made
 *  - compact:  one line — status, agent, duration. For graphs of dozens
 *  - hex:      a hexagon echoing the embedded telemetry star map. Densest
 */
function AgentNodeInner({ data, selected }: NodeProps<AgentFlowNode>) {
  const { node, blamed, style } = data
  const setReviewNode = useUi((s) => s.setReviewNode)

  const geometry = NODE_GEOMETRY[style]
  const statusColor = NODE_STATUS_VAR[node.status]
  const running = node.status === 'running'
  const waitingOnHuman = node.status === 'waiting' && node.waiting?.kind === 'human'

  const title = `${node.label} · ${NODE_STATUS_LABEL[node.status]}${
    node.durationMs !== undefined ? ` · ${formatDuration(node.durationMs)}` : ''
  }${node.model ? ` · ${node.model}` : ''}`

  if (style === 'hex') {
    return (
      <HexNode
        node={node}
        selected={Boolean(selected)}
        blamed={blamed}
        title={title}
        onReview={() => setReviewNode(node.nodeId)}
      />
    )
  }

  if (style === 'compact') {
    return (
      <div
        title={title}
        className={cn(
          'relative flex items-center overflow-hidden rounded-[var(--radius-control)] border bg-bg-2 select-none',
          '[transition-property:border-color,box-shadow] duration-[var(--dur-fast)] ease-[var(--ease-out-quint)]',
          running && 'node-sweep',
          selected
            ? 'border-accent shadow-[0_0_0_1px_var(--color-accent)]'
            : node.status === 'failed' || blamed
              ? 'border-st-failed/45'
              : 'border-line-2 hover:border-line-3',
        )}
        style={{ width: geometry.width, height: geometry.height }}
      >
        <Handle type="target" position={Position.Top} />
        <span
          aria-hidden
          className={cn('w-[3px] shrink-0 self-stretch', running && 'node-rail-pulse')}
          style={{ backgroundColor: statusColor }}
        />
        <span className="truncate-1 min-w-0 flex-1 px-2 text-[11px] text-fg-1">
          {node.label}
        </span>
        {node.attempts > 1 ? (
          <RotateCw size={9} strokeWidth={2} className="mr-1 shrink-0 text-st-waiting" />
        ) : null}
        {node.mocked ? (
          <FlaskConical size={9} strokeWidth={2} className="mr-1 shrink-0 text-st-waiting" />
        ) : null}
        <span
          className="num mono mr-2 shrink-0 text-[10px]"
          style={{ color: durationVar(node.durationMs) }}
        >
          {node.durationMs === undefined && running ? '···' : formatDuration(node.durationMs)}
        </span>
        {waitingOnHuman ? <ReviewPill compact onClick={() => setReviewNode(node.nodeId)} /> : null}
        <Handle type="source" position={Position.Bottom} />
      </div>
    )
  }

  return (
    <div
      className={cn(
        'relative flex overflow-hidden rounded-[var(--radius-node)] border bg-bg-2 select-none',
        '[transition-property:border-color,box-shadow,background-color] duration-[var(--dur-fast)] ease-[var(--ease-out-quint)]',
        running && 'node-sweep',
        selected
          ? 'border-accent shadow-[0_0_0_1px_var(--color-accent)]'
          : node.status === 'failed'
            ? 'border-st-failed/45'
            : blamed
              ? 'border-st-failed/30'
              : 'border-line-2 hover:border-line-3',
      )}
      style={{ width: geometry.width, height: geometry.height }}
    >
      <Handle type="target" position={Position.Top} />

      {/* Status rail: the thing you actually read when zoomed out. */}
      <span
        aria-hidden
        className={cn('w-[3px] shrink-0 rounded-l-[7px]', running && 'node-rail-pulse')}
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
              className="mono truncate-1 min-w-0 rounded-[3px] border border-line-2 px-1 text-2xs text-fg-3"
              title={node.model}
            >
              {shortModel(node.model)}
            </span>
          ) : (
            <span className="text-2xs text-fg-4">—</span>
          )}

          <span className="ml-auto flex shrink-0 items-center gap-1.5">
            {node.artifacts.length > 0 ? (
              <span
                className="num flex items-center gap-0.5 text-2xs text-fg-3"
                title={`${node.artifacts.length} artifact${node.artifacts.length === 1 ? '' : 's'} produced`}
              >
                <Package size={9} strokeWidth={2} />
                {node.artifacts.length}
              </span>
            ) : null}

            {node.attempts > 1 ? (
              <span
                className="num flex items-center gap-0.5 text-2xs text-st-waiting"
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

            {node.status === 'failed' && node.failure ? (
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
      {waitingOnHuman ? <ReviewPill onClick={() => setReviewNode(node.nodeId)} /> : null}

      <Handle type="source" position={Position.Bottom} />
    </div>
  )
}

/**
 * The projection copy-on-writes a fresh `data` object for every event batch, so
 * the default shallow compare never hits and all N nodes repaint on every push.
 * Comparing what the node actually draws means only the node that genuinely
 * changed repaints — which is what keeps zoom and pan smooth mid-run.
 */
export const AgentNode = memo(AgentNodeInner, (prev, next) => {
  if (prev.selected !== next.selected) return false
  if (prev.data.style !== next.data.style) return false
  if (prev.data.blamed !== next.data.blamed) return false

  const a = prev.data.node
  const b = next.data.node
  return (
    a.nodeId === b.nodeId &&
    a.label === b.label &&
    a.status === b.status &&
    a.model === b.model &&
    a.durationMs === b.durationMs &&
    a.attempts === b.attempts &&
    a.mocked === b.mocked &&
    a.artifacts.length === b.artifacts.length &&
    a.waiting?.kind === b.waiting?.kind &&
    a.failure?.reason === b.failure?.reason &&
    a.failure?.exitCode === b.failure?.exitCode
  )
})

/**
 * Hexagon, in the character of the embedded telemetry star map
 * (runtime/telemetry/ui/index.html:2717-2745): a dark well, a crisp status
 * stroke, and a small status core — and nothing else inside. Identity lives in
 * the label beneath; state lives in the stroke and the core.
 *
 * Drawn as one inline SVG rather than stacked `clip-path` divs. `clip-path`
 * discards borders, so the previous version faked its outline with a second
 * clipped layer behind, which reads as a thick soft ring at this size and
 * cannot antialias. A `<polygon>` gives a real 1.5px stroke.
 */
function HexNode({
  node,
  selected,
  blamed,
  title,
  onReview,
}: {
  node: RunNode
  selected: boolean
  blamed: boolean
  title: string
  onReview: () => void
}) {
  const { width, height } = NODE_GEOMETRY.hex
  const hexHeight = height - HEX_LABEL_HEIGHT
  const statusColor = NODE_STATUS_VAR[node.status]

  const running = node.status === 'running'
  const failed = node.status === 'failed'
  const pending = node.status === 'pending'
  const waitingOnHuman = node.status === 'waiting' && node.waiting?.kind === 'human'

  const stroke = selected
    ? 'var(--color-accent)'
    : failed || blamed
      ? 'var(--color-st-failed)'
      : statusColor

  // Leave room for the stroke and the selection ring so neither clips.
  const cx = width / 2
  const cy = hexHeight / 2
  const radius = Math.min(hexHeight / 2, width / 2) - 4

  return (
    <div
      title={title}
      className="relative flex select-none flex-col items-center"
      style={{ width, height }}
    >
      {/*
        Both handles are pinned to the hexagon's own vertices rather than the
        node box. The box is taller than the shape because it carries the label
        beneath, so a default bottom handle would launch every outgoing edge
        from under the text instead of from the point of the hexagon.
      */}
      <Handle type="target" position={Position.Top} style={{ top: 0 }} />

      <svg
        width={width}
        height={hexHeight}
        viewBox={`0 0 ${width} ${hexHeight}`}
        style={
          running || failed
            ? { filter: `drop-shadow(0 0 3px color-mix(in srgb, ${stroke} 55%, transparent))` }
            : undefined
        }
        aria-hidden
      >
        {selected ? (
          <polygon
            points={hexPoints(cx, cy, radius + 3)}
            fill="none"
            stroke="var(--color-accent)"
            strokeWidth={1}
            opacity={0.5}
          />
        ) : null}

        <polygon
          points={hexPoints(cx, cy, radius)}
          fill="var(--color-inset)"
          stroke={stroke}
          strokeWidth={selected ? 2 : 1.5}
          opacity={pending ? 0.65 : 1}
          strokeLinejoin="round"
        />

        {failed ? (
          <path
            d={`M ${cx - 3} ${cy - 3} L ${cx + 3} ${cy + 3} M ${cx + 3} ${cy - 3} L ${cx - 3} ${cy + 3}`}
            stroke={stroke}
            strokeWidth={1.6}
            strokeLinecap="round"
          />
        ) : pending || waitingOnHuman ? (
          <circle
            cx={cx}
            cy={cy}
            r={2.4}
            fill="none"
            stroke={statusColor}
            strokeWidth={1.3}
          />
        ) : (
          <circle
            cx={cx}
            cy={cy}
            r={2.8}
            fill={statusColor}
            className={running ? 'node-core-pulse' : undefined}
            style={{ transformOrigin: `${cx}px ${cy}px` }}
          />
        )}

        {/* RFC-026 §8: mock output must never be mistaken for real work. Sits on
            the upper-right vertex so the interior stays empty. */}
        {node.mocked ? (
          <circle
            cx={cx + radius * 0.87}
            cy={cy - radius * 0.5}
            r={2.6}
            fill="var(--color-st-waiting)"
            stroke="var(--color-inset)"
            strokeWidth={1}
          />
        ) : null}
      </svg>

      {/*
        A waiting node turns its label into the Review action rather than
        floating a pill over it — same footprint, no overlap. The agent name is
        still on the tooltip, in the inspector, and in the sidebar's approval list.
      */}
      {waitingOnHuman ? (
        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation()
            onReview()
          }}
          title={`${node.label} — review and approve or reject this checkpoint`}
          className={cn(
            'mt-px flex h-3.5 items-center rounded-[3px] bg-st-waiting px-1.5',
            'text-[8px] font-bold tracking-wide text-bg-0 uppercase',
            '[transition-property:filter] duration-[var(--dur-fast)] hover:brightness-110',
          )}
        >
          Review
        </button>
      ) : (
        <span className="flex w-full justify-center">
          {/*
            Edges leave the hexagon's bottom vertex and pass straight through
            this strip, so the label carries the canvas colour behind it to
            interrupt the line. Inline-block keeps that mask the width of the
            text rather than a 72px band across the graph.
          */}
          <span
            className="truncate-1 max-w-full rounded-[2px] bg-inset px-0.5 text-[9px] leading-[14px] text-fg-3"
            title={node.label}
          >
            {node.label}
          </span>
        </span>
      )}

      <Handle
        type="source"
        position={Position.Bottom}
        style={{ top: hexHeight, bottom: 'auto' }}
      />
    </div>
  )
}

function ReviewPill({
  onClick,
  compact,
}: {
  onClick: () => void
  compact?: boolean
}) {
  return (
    <button
      type="button"
      onClick={(event) => {
        event.stopPropagation()
        onClick()
      }}
      title="Review and approve or reject this checkpoint"
      className={cn(
        'flex items-center rounded-[3px] bg-st-waiting font-semibold tracking-wide text-bg-0 uppercase',
        '[transition-property:filter,transform] duration-[var(--dur-fast)]',
        'hover:brightness-110 active:scale-[0.95]',
        compact
          ? 'mr-1.5 h-[15px] shrink-0 px-1 text-[9px]'
          : 'absolute right-1.5 bottom-1.5 h-[18px] px-1.5 text-2xs',
      )}
    >
      Review
    </button>
  )
}

/** `groq/llama-3.1-8b-instant` reads better as `llama-3.1-8b-instant` on a chip. */
function shortModel(model: string): string {
  const slash = model.indexOf('/')
  return slash === -1 ? model : model.slice(slash + 1)
}

