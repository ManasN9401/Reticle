import dagre from '@dagrejs/dagre'
import type { Edge, Node } from '@xyflow/react'
import type { Run, RunNode } from '@shared/projection'

/**
 * Fixed node geometry.
 *
 * Nodes never resize — not on hover, not for a long agent id, not when a model
 * chip appears. A graph whose nodes change size relayouts under the user's
 * cursor, which makes a 60-node DAG impossible to read.
 */
export const NODE_WIDTH = 224
export const NODE_HEIGHT = 68

/** Ranks are the DAG's depth; separation is tuned so edges rarely cross. */
const RANK_SEPARATION = 64
const NODE_SEPARATION = 28

export interface AgentNodeData extends Record<string, unknown> {
  node: RunNode
  /** True when this node is the one the workflow blamed for failing the run. */
  blamed: boolean
}

export type AgentFlowNode = Node<AgentNodeData, 'agent'>

export type LayoutDirection = 'TB' | 'LR'

/**
 * Topological layout, satisfying RFC-022's architectural law 3: compute node
 * depth and sort to minimise edge crossover rather than placing nodes
 * arbitrarily.
 */
export function layoutGraph(
  run: Run,
  direction: LayoutDirection,
  pinned: Record<string, { x: number; y: number }> = {},
): { nodes: AgentFlowNode[]; edges: Edge[] } {
  const graph = new dagre.graphlib.Graph()
  graph.setGraph({
    rankdir: direction,
    ranksep: RANK_SEPARATION,
    nodesep: NODE_SEPARATION,
    marginx: 32,
    marginy: 32,
  })
  graph.setDefaultEdgeLabel(() => ({}))

  const nodeIds = Object.keys(run.nodes)
  // Alphabetical insertion gives dagre a deterministic tiebreak, so the same
  // DAG always lays out the same way (RFC-022 law 3's alphabetical fallback).
  for (const id of [...nodeIds].sort()) {
    graph.setNode(id, { width: NODE_WIDTH, height: NODE_HEIGHT })
  }

  const seen = new Set<string>()
  for (const edge of run.edges) {
    if (!run.nodes[edge.from] || !run.nodes[edge.to]) continue
    const key = `${edge.from}->${edge.to}`
    if (seen.has(key)) continue
    seen.add(key)
    graph.setEdge(edge.from, edge.to)
  }

  dagre.layout(graph)

  const nodes: AgentFlowNode[] = nodeIds.map((id) => {
    const runNode = run.nodes[id]
    const manual = pinned[id]
    const placed = graph.node(id) as { x: number; y: number } | undefined
    const position = manual ?? {
      // dagre reports centres; React Flow wants top-left.
      x: (placed?.x ?? 0) - NODE_WIDTH / 2,
      y: (placed?.y ?? 0) - NODE_HEIGHT / 2,
    }
    return {
      id,
      type: 'agent',
      position,
      data: { node: runNode, blamed: run.failureNodeId === id },
      width: NODE_WIDTH,
      height: NODE_HEIGHT,
    }
  })

  const edges: Edge[] = [...seen].map((key) => {
    const [from, to] = key.split('->')
    const source = run.nodes[from]
    // Animate only edges leaving a node that is genuinely working. The previous
    // build animated every edge unconditionally, which conveyed nothing.
    const live = source?.status === 'running'
    return {
      id: key,
      source: from,
      target: to,
      type: 'smoothstep',
      animated: live,
      style: {
        stroke: live ? 'var(--color-st-running)' : 'var(--color-line-2)',
        strokeWidth: live ? 1.6 : 1.2,
        opacity: live ? 0.9 : 0.75,
      },
    }
  })

  return { nodes, edges }
}
