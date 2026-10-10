import type { Run, RunNode } from '../../shared/projection'

/** What the approval request file says. Everything is optional: the file is audit-only text. */
export interface CheckpointInput {
  name?: string
  artifactId?: string
  data: unknown
}

export interface Checkpoint {
  prompt?: string
  protectedAction?: unknown
  inputs: CheckpointInput[]
  /** The JSON block parsed; false when it was missing or malformed. */
  parsed: boolean
}

const FENCE_OPEN = /^```json\s*$/
const FENCE_CLOSE = /^```\s*$/

/**
 * Read the JSON block of an approval request (runtime/agent/approval.go writes a readable
 * preface above it, which is ignored here). Never throws: a request that cannot be read
 * still opens, just without the structured view.
 */
export function parseCheckpoint(markdown: string): Checkpoint {
  const empty: Checkpoint = { inputs: [], parsed: false }
  const lines = markdown.replace(/\r\n?/g, '\n').split('\n')
  const start = lines.findIndex((line) => FENCE_OPEN.test(line))
  if (start === -1) return empty
  const end = lines.findIndex((line, index) => index > start && FENCE_CLOSE.test(line))
  const body = lines.slice(start + 1, end === -1 ? undefined : end).join('\n')
  let value: unknown
  try {
    value = JSON.parse(body)
  } catch {
    return empty
  }
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return empty
  const record = value as Record<string, unknown>
  const inputs: CheckpointInput[] = Array.isArray(record.inputs)
    ? record.inputs
        .filter((entry): entry is Record<string, unknown> => typeof entry === 'object' && entry !== null)
        .map((entry) => ({
          name: typeof entry.name === 'string' ? entry.name : undefined,
          artifactId: typeof entry.artifact_id === 'string' ? entry.artifact_id : undefined,
          data: entry.data,
        }))
    : []
  return {
    prompt: typeof record.prompt === 'string' ? record.prompt : undefined,
    protectedAction: record.protected_action,
    inputs,
    parsed: true,
  }
}

export const MAX_PLAN_STEPS = 40

export interface OutlineEntry {
  node: RunNode
  /** Distance from the start of the workflow, for indentation and ordering. */
  depth: number
  /** This is the checkpoint under review. */
  here: boolean
}

export interface PlanModel {
  /** The nodes feeding this checkpoint: the work being approved. */
  upstream: RunNode[]
  /** What runs after approval, in waves that can run in parallel. */
  waves: RunNode[][]
  /** Steps beyond MAX_PLAN_STEPS that are not listed. */
  hidden: number
  outline: OutlineEntry[]
}

/** Longest-path depth of every node reachable from `starts`, cycle-safe. */
function depths(starts: readonly string[], children: Map<string, string[]>, limit: number): Map<string, number> {
  const depth = new Map<string, number>(starts.map((id) => [id, 0]))
  // A relaxation pass per node is enough for any acyclic graph; the bound also stops cycles.
  for (let pass = 0; pass < limit; pass += 1) {
    let changed = false
    for (const [id, level] of [...depth]) {
      for (const child of children.get(id) ?? []) {
        if ((depth.get(child) ?? -1) < level + 1 && level + 1 <= limit) {
          depth.set(child, level + 1)
          changed = true
        }
      }
    }
    if (!changed) break
  }
  return depth
}

export function buildPlan(run: Run, nodeId: string): PlanModel {
  const children = new Map<string, string[]>()
  const parents = new Map<string, string[]>()
  for (const edge of run.edges) {
    if (!run.nodes[edge.from] || !run.nodes[edge.to]) continue
    children.set(edge.from, [...(children.get(edge.from) ?? []), edge.to])
    parents.set(edge.to, [...(parents.get(edge.to) ?? []), edge.from])
  }
  const byLabel = (a: RunNode, b: RunNode) => a.label.localeCompare(b.label) || a.nodeId.localeCompare(b.nodeId)
  const total = Object.keys(run.nodes).length

  const upstream = (parents.get(nodeId) ?? []).map((id) => run.nodes[id]).sort(byLabel)

  const after = depths([nodeId], children, total)
  after.delete(nodeId)
  const stepIds = [...after.keys()].sort((a, b) => (after.get(a)! - after.get(b)!) || byLabel(run.nodes[a], run.nodes[b]))
  const listed = stepIds.slice(0, MAX_PLAN_STEPS)
  const waves: RunNode[][] = []
  for (const id of listed) {
    const level = after.get(id)! - 1
    ;(waves[level] ??= []).push(run.nodes[id])
  }

  const roots = Object.keys(run.nodes).filter((id) => (parents.get(id) ?? []).length === 0)
  const overall = depths(roots, children, total)
  const outline = Object.values(run.nodes)
    .map((node) => ({ node, depth: overall.get(node.nodeId) ?? 0, here: node.nodeId === nodeId }))
    .sort((a, b) => a.depth - b.depth || byLabel(a.node, b.node))

  return { upstream, waves: waves.filter(Boolean), hidden: stepIds.length - listed.length, outline }
}

/** "3h 12m", "5m", "40s": how long a node has been waiting. */
export function formatWaiting(ms: number): string {
  const seconds = Math.max(0, Math.round(ms / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  return hours < 24 ? `${hours}h ${minutes % 60}m` : `${Math.floor(hours / 24)}d ${hours % 24}h`
}
