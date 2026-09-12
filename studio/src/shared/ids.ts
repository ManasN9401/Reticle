/**
 * Identity normalization for runtime events.
 *
 * The backend is inconsistent about how it names the same three identifiers,
 * so every consumer must go through here rather than reaching into payloads:
 *
 *  - exec id is `exec_id` on graph_engine events but `execution` on worker events
 *  - node id is only reliably available as the right half of `task_id`
 *  - the human-meaningful label is `worker_id` / `producer` / `agent_id`,
 *    never the generic `node-1` style node id (RFC-022, architectural law 5)
 */

/** The universal join key: `"{execId}|{nodeId}"` (agent/workflow_engine.go:250). */
export const TASK_ID_SEPARATOR = '|'

export const UNKNOWN_EXEC = 'unknown-exec'

export interface NodeIdentity {
  execId: string
  nodeId: string
  /** Composite `execId|nodeId`, stable across every subsystem. */
  taskId: string
  /** Display label — the semantic agent, not the generic node id. */
  agentId: string | undefined
}

function str(value: unknown): string | undefined {
  return typeof value === 'string' && value.length > 0 ? value : undefined
}

/** Split a composite task id. Returns undefined halves when it is not composite. */
export function parseTaskId(taskId: string | undefined): {
  execId?: string
  nodeId?: string
} {
  if (!taskId) return {}
  const index = taskId.indexOf(TASK_ID_SEPARATOR)
  if (index === -1) return { execId: taskId }
  return {
    execId: taskId.slice(0, index),
    nodeId: taskId.slice(index + TASK_ID_SEPARATOR.length),
  }
}

export function makeTaskId(execId: string, nodeId: string): string {
  return `${execId}${TASK_ID_SEPARATOR}${nodeId}`
}

/**
 * Resolve the identity a payload refers to, tolerating every naming variant.
 * Returns null when the payload carries no node identity at all (workflow-level
 * and runtime-level events).
 */
export function identify(payload: unknown): NodeIdentity | null {
  if (typeof payload !== 'object' || payload === null) return null
  const p = payload as Record<string, unknown>

  const rawTaskId = str(p.task_id) ?? str(p.id)
  const fromTask = parseTaskId(rawTaskId)

  const execId = str(p.exec_id) ?? str(p.execution) ?? fromTask.execId
  const agentId = str(p.worker_id) ?? str(p.producer) ?? str(p.agent_id)
  const nodeId = fromTask.nodeId ?? str(p.node_id) ?? agentId

  if (!execId || !nodeId) return null

  return {
    execId,
    nodeId,
    taskId: rawTaskId ?? makeTaskId(execId, nodeId),
    agentId,
  }
}

/** Resolve just the execution, for workflow-scoped events with no node. */
export function identifyExecution(payload: unknown): string | null {
  if (typeof payload !== 'object' || payload === null) return null
  const p = payload as Record<string, unknown>
  const direct = str(p.exec_id) ?? str(p.execution)
  if (direct) return direct
  const { execId } = parseTaskId(str(p.task_id) ?? str(p.id))
  return execId ?? null
}

// ---------------------------------------------------------------------------
// Out-of-band signals encoded in worker stderr
// ---------------------------------------------------------------------------

/**
 * Workers signal UI-only states by printing a sentinel to stderr, which the
 * runtime relays as a `WorkerLog` event. These are not distinct event types.
 *
 *  - `WAITING_HUMAN` — a hitl-agent checkpoint is blocking the DAG (RFC-038)
 *  - `WAITING_COMFY` — blocked on an external ComfyUI round trip
 *  - `RESUMED`       — the block cleared
 */
const UI_STATE_PATTERN = /\[UI_STATE:\s*([A-Z_]+)\s*\]/

export type UiStateSignal = 'WAITING_HUMAN' | 'WAITING_COMFY' | 'RESUMED'

export function parseUiState(log: string | undefined): UiStateSignal | null {
  if (!log) return null
  const match = UI_STATE_PATTERN.exec(log)
  if (!match) return null
  const signal = match[1]
  if (signal === 'WAITING_HUMAN' || signal === 'WAITING_COMFY' || signal === 'RESUMED') {
    return signal
  }
  return null
}

/**
 * The hitl-agent logs the absolute path of the approval file it just wrote
 * (cmd/forge/compiler/agents/hitl-agent/workers/hitl.py:57). Capturing it from
 * the log stream is what lets Studio drive approvals without depending on the
 * agent's hardcoded checkpoint directory being correct.
 */
const CHECKPOINT_PATTERN = /Checkpoint file created at:\s*(.+?)\s*$/

export function parseCheckpointPath(log: string | undefined): string | null {
  if (!log) return null
  const match = CHECKPOINT_PATTERN.exec(log.trim())
  return match ? match[1] : null
}

/**
 * Workers fall back to mock output when a model is unavailable. RFC-026 §8
 * requires the UI to make that obvious rather than presenting mocks as real work.
 */
const MOCK_PATTERN = /\b(mock fallback|falling back to mock|\[MOCK\])/i

export function indicatesMockFallback(log: string | undefined): boolean {
  return log ? MOCK_PATTERN.test(log) : false
}

/** Worker LLM diagnostics are prefixed `[LLM]` or `[LLM_STREAM]`; shown in a separate inspector tab. */
export function isLlmLog(log: string | undefined): boolean {
  return log ? log.includes('[LLM]') || log.includes('[LLM_STREAM]') : false
}
