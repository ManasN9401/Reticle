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
import { LayoutGrid, Maximize, Waypoints } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { cn } from '@/design/cn'
import { EmptyState, IconButton, Tooltip } from '@/design/primitives'
import { NODE_STATUS_LABEL, NODE_STATUS_VAR } from '@/design/status'
import { registerGraphHandler } from '@/app/commands'
import { useStudio } from '@/state/store'
import { runTotals, type NodeStatus } from '@shared/projection'
import { AgentNode } from './AgentNode'
import { Timeline } from './Timeline'
import { layoutGraph, type AgentFlowNode, type LayoutDirection } from './layout'
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

  const { nodes, edges } = useMemo(() => {
    if (!run) return { nodes: [] as AgentFlowNode[], edges: [] as Edge[] }
    const laid = layoutGraph(run, direction, pinned)
    if (hidden.size === 0) return laid

    const visible = new Set(
      laid.nodes.filter((n) => !hidden.has(n.data.node.status)).map((n) => n.id),
    )
    return {
      nodes: laid.nodes.filter((n) => visible.has(n.id)),
      edges: laid.edges.filter((e) => visible.has(e.source) && visible.has(e.target)),
    }
  }, [run, direction, pinned, hidden])

  const selectedNodes = useMemo(
    () => nodes.map((node) => ({ ...node, selected: node.id === selectedNodeId })),
    [nodes, selectedNodeId],
  )

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
            nodes={selectedNodes}
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
              maskColor="rgba(8,9,11,0.72)"
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
