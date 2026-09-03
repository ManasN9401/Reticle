import dagre from '@dagrejs/dagre'
import type { Edge, Node } from '@xyflow/react'
import type { Run, RunNode } from '@shared/projection'
import type { NodeStyle } from '@shared/ipc'

/**
 * Fixed node geometry, per presentation style.
 *
 * Within a style nodes never resize — not on hover, not for a long agent id,
 * not when a model chip appears. A graph whose nodes change size relayouts
 * under the user's cursor, which makes a 60-node DAG impossible to read.
 *
 * Separation is tuned per style rather than shared: the spacing that keeps
 * 224px cards legible leaves hexagons swimming in emptiness.
 */
export const NODE_GEOMETRY: Record<
  NodeStyle,
  { width: number; height: number; rankSep: number; nodeSep: number }
> = {
  detailed: { width: 224, height: 68, rankSep: 64, nodeSep: 28 },
  compact: { width: 168, height: 30, rankSep: 44, nodeSep: 14 },
  // The box is wider than the hexagon so the label beneath has room; the shape
  // itself is only ~36x42. Nothing is drawn inside it, which is what lets it be
  // this small.
  hex: { width: 72, height: 58, rankSep: 30, nodeSep: 10 },
}

/** Height of the label strip beneath a hexagon, inside its box. */
export const HEX_LABEL_HEIGHT = 14

/**
 * Vertices of a pointy-top hexagon, using the same formula as the embedded star
 * map (runtime/telemetry/ui/index.html:2706) so the two consoles draw the shape
 * at the same orientation.
 */
export function hexPoints(cx: number, cy: number, r: number): string {
  const points: string[] = []
  for (let i = 0; i < 6; i += 1) {
    const angle = (Math.PI / 3) * i - Math.PI / 2
    points.push(`${(cx + r * Math.cos(angle)).toFixed(2)},${(cy + r * Math.sin(angle)).toFixed(2)}`)
  }
  return points.join(' ')
}

export interface AgentNodeData extends Record<string, unknown> {
  node: RunNode
  /** True when this node is the one the workflow blamed for failing the run. */
  blamed: boolean
  style: NodeStyle
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
  style: NodeStyle,
  pinned: Record<string, { x: number; y: number }> = {},
): { nodes: AgentFlowNode[]; edges: Edge[] } {
  const geometry = NODE_GEOMETRY[style]
  const graph = new dagre.graphlib.Graph()
  graph.setGraph({
    rankdir: direction,
    ranksep: geometry.rankSep,
    nodesep: geometry.nodeSep,
    marginx: 32,
    marginy: 32,
  })
  graph.setDefaultEdgeLabel(() => ({}))

  const nodeIds = Object.keys(run.nodes)
  // Alphabetical insertion gives dagre a deterministic tiebreak, so the same
  // DAG always lays out the same way (RFC-022 law 3's alphabetical fallback).
  for (const id of [...nodeIds].sort()) {
    graph.setNode(id, { width: geometry.width, height: geometry.height })
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
      x: (placed?.x ?? 0) - geometry.width / 2,
      y: (placed?.y ?? 0) - geometry.height / 2,
    }
    return {
      id,
      type: 'agent',
      position,
      data: { node: runNode, blamed: run.failureNodeId === id, style },
      width: geometry.width,
      height: geometry.height,
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
