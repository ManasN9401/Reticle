import type { NodeStatus, RunStatus } from '@shared/projection'
import type { ExecutionStatus } from '@shared/events'
import type { ConnectionPhase } from '@shared/ipc'

/**
 * Status is the only thing on screen allowed to carry saturated colour, so
 * every mapping to a hue lives here — one place to audit that the rule holds.
 */

export const NODE_STATUS_VAR: Record<NodeStatus, string> = {
  pending: 'var(--color-st-idle)',
  running: 'var(--color-st-running)',
  done: 'var(--color-st-done)',
  failed: 'var(--color-st-failed)',
  waiting: 'var(--color-st-waiting)',
}

export const NODE_STATUS_WEAK_VAR: Record<NodeStatus, string> = {
  pending: 'var(--color-st-idle-weak)',
  running: 'var(--color-st-running-weak)',
  done: 'var(--color-st-done-weak)',
  failed: 'var(--color-st-failed-weak)',
  waiting: 'var(--color-st-waiting-weak)',
}

export const NODE_STATUS_LABEL: Record<NodeStatus, string> = {
  pending: 'Pending',
  running: 'Running',
  done: 'Done',
  failed: 'Failed',
  waiting: 'Waiting',
}

const RUN_TO_NODE: Record<RunStatus, NodeStatus> = {
  pending: 'pending',
  running: 'running',
  completed: 'done',
  failed: 'failed',
}

export function runStatusVar(status: RunStatus): string {
  return NODE_STATUS_VAR[RUN_TO_NODE[status]]
}

const EXECUTION_TO_NODE: Record<ExecutionStatus, NodeStatus> = {
  PENDING: 'pending',
  RUNNING: 'running',
  COMPLETED: 'done',
  FAILED: 'failed',
}

export function executionStatusVar(status: ExecutionStatus): string {
  return NODE_STATUS_VAR[EXECUTION_TO_NODE[status] ?? 'pending']
}

export function connectionVar(phase: ConnectionPhase): string {
  switch (phase) {
    case 'connected':
      return 'var(--color-st-done)'
    case 'connecting':
    case 'reconnecting':
      return 'var(--color-st-waiting)'
    case 'error':
      return 'var(--color-st-failed)'
    default:
      return 'var(--color-st-idle)'
  }
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

/**
 * Durations are derived client-side — the backend never sends one. Formatting
 * keeps a fixed number of significant digits so the column does not jitter as
 * values change (paired with `tabular-nums`).
 */
export function formatDuration(ms: number | undefined): string {
  if (ms === undefined) return '—'
  if (ms < 1000) return `${Math.round(ms)}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)}s`
  const minutes = Math.floor(ms / 60_000)
  const seconds = Math.floor((ms % 60_000) / 1000)
  return `${minutes}m ${String(seconds).padStart(2, '0')}s`
}

/**
 * Fast nodes read cyan, ordinary ones neutral, slow ones recede — the same
 * grading the star map uses, so the two consoles stay legible together.
 */
export function durationVar(ms: number | undefined): string {
  if (ms === undefined) return 'var(--color-fg-4)'
  if (ms < 1000) return 'var(--color-st-running)'
  if (ms < 5000) return 'var(--color-fg-2)'
  return 'var(--color-fg-3)'
}

export function formatClock(at: number): string {
  const d = new Date(at)
  const pad = (n: number, w = 2) => String(n).padStart(w, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(
    d.getMilliseconds(),
    3,
  )}`
}

export function formatRelative(at: number | undefined, now = Date.now()): string {
  if (at === undefined) return '—'
  const delta = Math.max(0, now - at)
  if (delta < 1000) return 'now'
  if (delta < 60_000) return `${Math.floor(delta / 1000)}s ago`
  if (delta < 3_600_000) return `${Math.floor(delta / 60_000)}m ago`
  if (delta < 86_400_000) return `${Math.floor(delta / 3_600_000)}h ago`
  return `${Math.floor(delta / 86_400_000)}d ago`
}

export function formatBytes(bytes: number | undefined): string {
  if (bytes === undefined) return '—'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

/** Worker failure reasons are snake_case on the wire; make them readable. */
export function formatFailureReason(reason: string): string {
  switch (reason) {
    case 'exit_non_zero':
      return 'Exited non-zero'
    case 'protocol_error':
      return 'Protocol error'
    case 'invalid_json':
      return 'Invalid JSON response'
    case 'timeout':
      return 'Timed out'
    case 'panic':
      return 'Panicked'
    case 'start_failed':
      return 'Failed to start'
    default:
      return reason
  }
}
