import dagre from '@dagrejs/dagre'
import type { Edge, Node } from '@xyflow/react'
import type { Run, RunNode } from '@shared/projection'
import type { NodeAppearanceSettings, NodeStyle } from '@shared/ipc'

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
  // Orthogonal routing needs roughly 2x the corner radius of vertical clearance
  // to round its elbows; below that it degenerates into flat staples. That, not
  // the routing mode, is what made the first hexagon pass look wrong at 30px.
  hex: { width: 72, height: 58, rankSep: 50, nodeSep: 16 },
}

/** Corner radius for the elbow routing. */
const EDGE_CORNER_RADIUS = 8

/** Height of the label strip beneath a hexagon, inside its box, at scale 1. */
export const HEX_LABEL_HEIGHT = 14

export interface ResolvedNodeGeometry {
  width: number
  height: number
  rankSep: number
  nodeSep: number
  /** Also exposed so render code can scale derived values (hex label strip, etc.) consistently. */
  scale: number
}

/**
 * `NODE_GEOMETRY[style]` scaled uniformly by the user's per-style size
 * preference (default 1, clamped 0.85–1.35 in settings validation). Uniform
 * scaling — never an independent per-dimension knob — is what keeps the
 * hand-tuned proportions above (edge corner clearance, hex label strip) in
 * the same ratio they were tuned at.
 */
export function getNodeGeometry(
  style: NodeStyle,
  appearance?: NodeAppearanceSettings,
): ResolvedNodeGeometry {
  const base = NODE_GEOMETRY[style]
  const scale = appearance?.[style].scale ?? 1
  if (scale === 1) return { ...base, scale: 1 }
  return {
    width: Math.round(base.width * scale),
    height: Math.round(base.height * scale),
    rankSep: Math.round(base.rankSep * scale),
    nodeSep: Math.round(base.nodeSep * scale),
    scale,
  }
}

/**
 * Vertices of a regular, point-up polygon. Defaults to 6 sides (a hexagon),
 * using the same formula as the embedded star map
 * (runtime/telemetry/ui/index.html:2706) so the two consoles draw the shape at
 * the same orientation; the octagon node-appearance option reuses this with
 * `sides: 8`.
 */
export function hexPoints(cx: number, cy: number, r: number, sides = 6): string {
  const points: string[] = []
  for (let i = 0; i < sides; i += 1) {
    const angle = ((2 * Math.PI) / sides) * i - Math.PI / 2
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

/** Where dagre placed each node, plus the de-duplicated edge list it used. */
export interface GraphLayout {
  positions: Record<string, { x: number; y: number }>
  edgeKeys: string[]
}

/**
 * Topological layout, satisfying RFC-022's architectural law 3: compute node
 * depth and sort to minimise edge crossover rather than placing nodes
 * arbitrarily.
 *
 * Deliberately separate from node construction. Positions depend only on the
 * *topology* — ids, edges, direction, style, manual overrides — never on
 * status, duration or artifacts. Keeping dagre behind that boundary is what
 * stops a live run re-laying out the whole graph on every event batch.
 */
export function layoutPositions(
  run: Run,
  direction: LayoutDirection,
  style: NodeStyle,
  appearance?: NodeAppearanceSettings,
  pinned: Record<string, { x: number; y: number }> = {},
): GraphLayout {
  const geometry = getNodeGeometry(style, appearance)
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

  const positions: Record<string, { x: number; y: number }> = {}
  for (const id of nodeIds) {
    const manual = pinned[id]
    if (manual) {
      positions[id] = manual
      continue
    }
    const placed = graph.node(id) as { x: number; y: number } | undefined
    // dagre reports centres; React Flow wants top-left.
    positions[id] = {
      x: (placed?.x ?? 0) - geometry.width / 2,
      y: (placed?.y ?? 0) - geometry.height / 2,
    }
  }

  return { positions, edgeKeys: [...seen] }
}

/**
 * Build React Flow nodes and edges from a cached layout plus the live run.
 * Cheap enough to run on every state push — it is object construction only.
 */
export function buildGraph(
  run: Run,
  layout: GraphLayout,
  style: NodeStyle,
  selectedNodeId: string | null,
  appearance?: NodeAppearanceSettings,
): { nodes: AgentFlowNode[]; edges: Edge[] } {
  const geometry = getNodeGeometry(style, appearance)

  const nodes: AgentFlowNode[] = Object.keys(run.nodes).map((id) => ({
    id,
    type: 'agent',
    position: layout.positions[id] ?? { x: 0, y: 0 },
    selected: id === selectedNodeId,
    data: { node: run.nodes[id], blamed: run.failureNodeId === id, style },
    width: geometry.width,
    height: geometry.height,
  }))

  const edges: Edge[] = layout.edgeKeys.map((key) => {
    const [from, to] = key.split('->')
    // Animate only edges leaving a node that is genuinely working. The previous
    // build animated every edge unconditionally, which conveyed nothing.
    const live = run.nodes[from]?.status === 'running'
    return {
      id: key,
      source: from,
      target: to,
      // Elbow routing with rounded corners. It needs the vertical clearance
      // that NODE_GEOMETRY reserves — starved of room it flattens into staples.
      type: 'smoothstep',
      pathOptions: { borderRadius: EDGE_CORNER_RADIUS },
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
