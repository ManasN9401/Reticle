import {
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Edge,
  type NodeChange,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import './graph.css'
import { Hexagon, LayoutGrid, Maximize, Rows3, SquareStack, Waypoints } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { cn } from '@/design/cn'
import { EmptyState, IconButton, Tooltip } from '@/design/primitives'
import { NODE_STATUS_LABEL, NODE_STATUS_VAR } from '@/design/status'
import { registerGraphHandler } from '@/app/commands'
import { bridge } from '@/state/bridge'
import { useStudio } from '@/state/store'
import { useResolvedTheme } from '@/state/theme'
import { runTotals, type NodeStatus } from '@shared/projection'
import type { NodeStyle } from '@shared/ipc'
import { AgentNode } from './AgentNode'
import { Timeline } from './Timeline'
import { buildGraph, layoutPositions, type AgentFlowNode, type LayoutDirection } from './layout'
import { useScrubbedRun } from './useScrubbedRun'

const NODE_TYPES = { agent: AgentNode }
const STATUSES: NodeStatus[] = ['pending', 'running', 'done', 'failed', 'waiting']

export function GraphCanvas() {
  return (
    <ReactFlowProvider>
      <GraphCanvasInner />
    </ReactFlowProvider>
  )
}

function GraphCanvasInner() {
  const { run, replaying } = useScrubbedRun()
  const selectedNodeId = useStudio((s) => s.selectedNodeId)
  const selectNode = useStudio((s) => s.selectNode)
  const { fitView } = useReactFlow()

  const nodeStyle = useStudio((s) => s.settings?.appearance.nodeStyle ?? 'detailed')
  const theme = useResolvedTheme()
  const [direction, setDirection] = useState<LayoutDirection>('TB')
  const [hidden, setHidden] = useState<Set<NodeStatus>>(new Set())
  /** Manual positions survive re-renders but are cleared by an explicit re-layout. */
  const [pinned, setPinned] = useState<Record<string, { x: number; y: number }>>({})
  const lastExecId = useRef<string | undefined>(undefined)

  // A different run is a different graph — drop manual placement and refit.
  useEffect(() => {
    if (run?.execId !== lastExecId.current) {
      lastExecId.current = run?.execId
      setPinned({})
      if (run) requestAnimationFrame(() => fitView({ padding: 0.18, duration: 200 }))
    }
  }, [run?.execId, run, fitView])

  /**
   * Dagre runs only when the *topology* changes, never when a status or
   * duration does. The reducer preserves `run.edges` by reference across
   * copy-on-writes (`draftRun` in shared/projection.ts), so identity here is an
   * O(1) proxy for "the shape of the graph changed" — and the node count covers
   * a node seeded outside the edge list. Without this, a live run re-lays out
   * the whole graph on every event batch and starves zoom and pan of frames.
   */
  const nodeCount = run ? Object.keys(run.nodes).length : 0
  const layout = useMemo(
    () => (run ? layoutPositions(run, direction, nodeStyle, pinned) : null),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed on topology, not on `run` identity
    [run?.execId, run?.edges, nodeCount, direction, nodeStyle, pinned],
  )

  const { nodes, edges } = useMemo(() => {
    if (!run || !layout) return { nodes: [] as AgentFlowNode[], edges: [] as Edge[] }
    const built = buildGraph(run, layout, nodeStyle, selectedNodeId)
    if (hidden.size === 0) return built

    const visible = new Set(
      built.nodes.filter((n) => !hidden.has(n.data.node.status)).map((n) => n.id),
    )
    return {
      nodes: built.nodes.filter((n) => visible.has(n.id)),
      edges: built.edges.filter((e) => visible.has(e.source) && visible.has(e.target)),
    }
  }, [run, layout, nodeStyle, selectedNodeId, hidden])

  const onNodesChange = useCallback((changes: NodeChange<AgentFlowNode>[]) => {
    // Only positions are persisted; everything else is derived from the run.
    setPinned((prev) => {
      let next = prev
      for (const change of changes) {
        if (change.type === 'position' && change.position) {
          if (next === prev) next = { ...prev }
          next[change.id] = change.position
        }
      }
      return next
    })
  }, [])

  const setNodeStyle = useCallback(
    (style: NodeStyle) => {
      setPinned({})
      void bridge?.settings.patch({ appearance: { nodeStyle: style } })
      requestAnimationFrame(() => fitView({ padding: 0.18, duration: 220 }))
    },
    [fitView],
  )

  const relayout = useCallback(() => {
    setPinned({})
    requestAnimationFrame(() => fitView({ padding: 0.18, duration: 220 }))
  }, [fitView])

  const fit = useCallback(() => {
    fitView({ padding: 0.18, duration: 220 })
  }, [fitView])

  // Expose the graph's actions to the shared command registry while mounted, so
  // the menu, palette and keybindings can drive it.
  useEffect(() => {
    registerGraphHandler('graph.relayout', relayout)
    registerGraphHandler('graph.fit', fit)
    return () => {
      registerGraphHandler('graph.relayout', null)
      registerGraphHandler('graph.fit', null)
    }
  }, [relayout, fit])

  const totals = runTotals(run)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex h-[var(--h-toolbar)] shrink-0 items-center gap-1 border-b border-line-1 bg-bg-1 px-2">
        <Tooltip content="Re-layout (Ctrl+Alt+L)">
          <IconButton label="Re-layout" size="sm" onClick={relayout} disabled={!run}>
            <LayoutGrid size={13} strokeWidth={1.7} />
          </IconButton>
        </Tooltip>
        <Tooltip content="Fit to view (Ctrl+Alt+F)">
          <IconButton label="Fit to view" size="sm" onClick={fit} disabled={!run}>
            <Maximize size={13} strokeWidth={1.7} />
          </IconButton>
        </Tooltip>
        <Tooltip content={direction === 'TB' ? 'Switch to left-to-right' : 'Switch to top-down'}>
          <IconButton
            label="Toggle orientation"
            size="sm"
            disabled={!run}
            onClick={() => {
              setPinned({})
              setDirection((d) => (d === 'TB' ? 'LR' : 'TB'))
              requestAnimationFrame(() => fitView({ padding: 0.18, duration: 220 }))
            }}
          >
            <Waypoints
              size={13}
              strokeWidth={1.7}
              className={direction === 'LR' ? 'rotate-90' : undefined}
            />
          </IconButton>
        </Tooltip>

        <span className="mx-1 h-4 w-px bg-line-2" />

        {/* Presentation switch. Persisted, so it survives a restart. */}
        <div className="flex items-center gap-px rounded-[var(--radius-control)] border border-line-2 p-px">
          {(
            [
              ['detailed', 'Detailed nodes', SquareStack],
              ['compact', 'Compact nodes', Rows3],
              ['hex', 'Hexagonal nodes', Hexagon],
            ] as const
          ).map(([style, label, Icon]) => (
            <button
              key={style}
              type="button"
              aria-pressed={nodeStyle === style}
              title={label}
              disabled={!run}
              onClick={() => setNodeStyle(style)}
              className={cn(
                'flex h-[22px] w-[26px] items-center justify-center rounded-[3px]',
                '[transition-property:background-color,color] duration-[var(--dur-fast)]',
                'disabled:pointer-events-none disabled:opacity-30',
                nodeStyle === style
                  ? 'bg-bg-3 text-fg-1'
                  : 'text-fg-4 hover:bg-bg-2 hover:text-fg-2',
              )}
            >
              <Icon size={13} strokeWidth={1.8} />
            </button>
          ))}
        </div>

        <span className="mx-1 h-4 w-px bg-line-2" />

        {/* Status filter doubles as the legend — one control, two jobs. */}
        <div className="flex items-center gap-0.5">
          {STATUSES.map((status) => {
            const count = totals[status]
            const off = hidden.has(status)
            return (
              <button
                key={status}
                type="button"
                aria-pressed={!off}
                disabled={!run}
                title={`${off ? 'Show' : 'Hide'} ${NODE_STATUS_LABEL[status].toLowerCase()} nodes`}
                onClick={() =>
                  setHidden((prev) => {
                    const next = new Set(prev)
                    if (next.has(status)) next.delete(status)
                    else next.add(status)
                    return next
                  })
                }
                className={cn(
                  'flex h-6 items-center gap-1.5 rounded-[3px] px-1.5 text-2xs',
                  '[transition-property:opacity,background-color,color] duration-[var(--dur-fast)]',
                  'disabled:pointer-events-none disabled:opacity-30',
                  off ? 'text-fg-4 opacity-45' : 'text-fg-2 hover:bg-bg-2',
                )}
              >
                <span
                  className="h-1.5 w-1.5 rounded-full"
                  style={{ backgroundColor: NODE_STATUS_VAR[status] }}
                />
                <span className="num">{count}</span>
              </button>
            )
          })}
        </div>

        {replaying ? (
          <span className="ml-auto rounded-[3px] bg-st-waiting-weak px-2 py-0.5 text-2xs text-st-waiting">
            Replaying historical state
          </span>
        ) : null}
      </div>

      <div className="relative min-h-0 flex-1">
        {!run ? (
          <div className="h-full bg-inset">
            <EmptyState
              icon={<Waypoints size={26} strokeWidth={1.3} />}
              title="No run to display"
              description="Submit a prompt from the Runs sidebar, or connect to a forge instance that is already executing a workflow. The DAG appears here as soon as the architect emits it."
            />
          </div>
        ) : (
          <ReactFlow
            className="reticle-flow"
            nodes={nodes}
            edges={edges}
            nodeTypes={NODE_TYPES}
            onNodesChange={onNodesChange}
            onNodeClick={(_event, node) => selectNode(node.id)}
            onPaneClick={() => selectNode(null)}
            nodesDraggable
            nodesConnectable={false}
            elementsSelectable
            proOptions={{ hideAttribution: true }}
            minZoom={0.15}
            maxZoom={2.5}
            fitView
            fitViewOptions={{ padding: 0.18 }}
          >
            <Background
              variant={BackgroundVariant.Dots}
              gap={18}
              size={1}
              color="var(--color-line-1)"
            />
            <Controls showInteractive={false} position="bottom-left" />
            <MiniMap
              pannable
              zoomable
              position="bottom-right"
              maskColor={
                theme === 'light' ? 'rgba(238,240,244,0.75)' : 'rgba(8,9,11,0.72)'
              }
              nodeColor={(node) =>
                NODE_STATUS_VAR[
                  ((node as AgentFlowNode).data?.node.status ?? 'pending') as NodeStatus
                ]
              }
              nodeStrokeWidth={0}
              style={{ width: 148, height: 96 }}
            />
          </ReactFlow>
        )}
      </div>

      <Timeline />
    </div>
  )
}
