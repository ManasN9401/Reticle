/**
 * Wire types for the Reticle runtime event bus.
 *
 * Two encodings of the same events exist and they are NOT interchangeable:
 *
 *  - WebSocket frames (`/ws`) marshal Go's `events.RuntimeEvent` directly. That
 *    struct carries no json tags (runtime/events/bus.go:15-22), so the envelope
 *    keys arrive **capitalized**: `ID`, `Type`, `Timestamp`, `Source`, ...
 *  - `logs/runtime.log` is written by slog and uses lowercase `time`, `level`,
 *    `msg`, `component`, `event`, `payload`.
 *
 * `decodeSocketEvent` and `decodeLogLine` normalize both into `RuntimeEvent`.
 */

export type EventType =
  | 'RuntimeOverloaded'
  | 'RuntimePersistenceFailed'
  | 'WorkflowSnapshot'
  | 'ExecutionPaused'
  | 'ExecutionResumed'
  | 'ExecutionKilled'
  | 'EnvironmentProvisioningCompleted'
  | 'EnvironmentProvisioningFailed'
  | 'EnvironmentProvisioningStarted'
  | 'ArtifactStored'
  | 'ArtifactVersionCreated'
  | 'ArtifactsProduced'
  | 'AutomationTriggered'
  | 'FileLockReleased'
  | 'FileLockRequested'
  | 'GraphMutationRequested'
  | 'MemoryReadCompleted'
  | 'MemoryReadRequested'
  | 'MemoryUpdated'
  | 'MemoryWriteRequested'
  | 'NodeReady'
  | 'RuntimeShutdown'
  | 'RuntimeStarted'
  | 'TaskCreated'
  | 'TaskDispatched'
  | 'TaskFailed'
  | 'WaitlistCommand'
  | 'WaitlistStateRequested'
  | 'WaitlistUpdated'
  | 'WorkerCompleted'
  | 'WorkerVerificationRecorded'
  | 'WorkerFailed'
  | 'WorkerLog'
  | 'WorkerStarted'
  | 'WorkflowCompleted'
  | 'WorkflowFailed'
  | 'WorkflowStarted'
  | 'WorkflowStateRequested'

/** Every event type the runtime is known to publish. */
export const EVENT_TYPES: readonly EventType[] = [
  'RuntimeOverloaded',
  'RuntimePersistenceFailed',
  'WorkflowSnapshot',
  'ExecutionPaused',
  'ExecutionResumed',
  'ExecutionKilled',
  'EnvironmentProvisioningCompleted',
  'EnvironmentProvisioningFailed',
  'EnvironmentProvisioningStarted',
  'ArtifactStored',
  'ArtifactVersionCreated',
  'ArtifactsProduced',
  'AutomationTriggered',
  'FileLockReleased',
  'FileLockRequested',
  'GraphMutationRequested',
  'MemoryReadCompleted',
  'MemoryReadRequested',
  'MemoryUpdated',
  'MemoryWriteRequested',
  'NodeReady',
  'RuntimeShutdown',
  'RuntimeStarted',
  'TaskCreated',
  'TaskDispatched',
  'TaskFailed',
  'WaitlistCommand',
  'WaitlistStateRequested',
  'WaitlistUpdated',
  'WorkerCompleted',
  'WorkerVerificationRecorded',
  'WorkerFailed',
  'WorkerLog',
  'WorkerStarted',
  'WorkflowCompleted',
  'WorkflowFailed',
  'WorkflowStarted',
  'WorkflowStateRequested',
]

/** Normalized envelope used everywhere in Studio. */
export interface RuntimeEvent<P = unknown> {
  id: number
  type: EventType | string
  /** Epoch milliseconds. */
  timestamp: number
  source: string
  sessionId: string
  payload: P
}

// ---------------------------------------------------------------------------
// Payload shapes
// ---------------------------------------------------------------------------

/** Current runtime events use lowercase JSON fields. Uppercase fields remain
 * accepted when replaying logs written by older Reticle versions. */
export interface WireEdge {
  from?: string
  to?: string
  From?: string
  To?: string
}

/** Payload for the WorkflowStarted event, representing the initialization of a DAG. */
export interface WorkflowStartedPayload {
  workflow_id?: string
  exec_id?: string
  edges?: WireEdge[]
}

/** Payload for the NodeReady event, triggered when a node's dependencies are satisfied. */
export interface NodeReadyPayload {
  node_id?: string
  exec_id?: string
  workflow?: string
}

/** Payload for the TaskDispatched event, recording which model and worker took a task. */
export interface TaskDispatchedPayload {
  task_id?: string
  worker_id?: string
  llm_model?: string
}

/** Payload for standard worker lifecycle events (WorkerStarted, WorkerCompleted). */
export interface WorkerLifecyclePayload {
  task_id?: string
  worker_id?: string
}

/** Payload for the WorkerLog event, containing streaming stderr from the worker. */
export interface WorkerLogPayload {
  task_id?: string
  worker_id?: string
  log?: string
}

/** runtime/agent/worker.go:30-37 */
export type WorkerFailureReason =
  | 'exit_non_zero'
  | 'protocol_error'
  | 'invalid_json'
  | 'timeout'
  | 'panic'
  | 'start_failed'

/** Payload for the WorkerFailed event, containing error details and terminal output. */
export interface WorkerFailedPayload {
  task_id?: string
  worker_id?: string
  reason?: WorkerFailureReason | string
  exit_code?: number
  stderr?: string
}

/** Payload for terminal workflow events (WorkflowCompleted, WorkflowFailed). */
export interface WorkflowTerminalPayload {
  workflow?: string
  execution?: string
  reason?: string
  node_id?: string
}

/** runtime/memory/types.go:15-27 */
export interface Artifact {
  id: string
  name?: string
  type?: string
  producer?: string
  workflow?: string
  execution?: string
  task?: string
  parents?: string[]
  created_at?: string
  stored_at?: string | null
  version?: number
  data?: unknown
}

/** cmd/forge/waitlist.go:22-36 */
export type ExecutionStatus = 'PENDING' | 'RUNNING' | 'PAUSED' | 'COMPLETED' | 'FAILED'
export type ExecutionMode = 'parallel' | 'sequential'

export interface Attachment {
  id: string
  filename: string
  mime_type: string
  path: string
}

/** Represents a pending or active instruction in the execution waitlist. */
export interface WaitlistItem {
  id: string
  prompt: string
  status: ExecutionStatus
  group?: string
  mode?: ExecutionMode
  ide_context?: string
  effort?: string
  agent_complexity?: number
  attachments?: Attachment[]
  created_at?: string
}

/** Payload for the WaitlistUpdated event, broadcasting the full queue state. */
export interface WaitlistPayload {
  items: WaitlistItem[] | null
  maxWorkers: number
  runningWorkers: number
  /**
   * Absent on the first frame after connect: the state-replay path
   * (cmd/forge/waitlist.go:262-266) builds the payload without it, while the
   * save path (:380-385) includes it. Always treat as nullable.
   */
  lockedKeys: string[] | null
}

/** runtime/routing/models.go:18-25 — served by `GET /api/models`. */
export interface RoutingModel {
  id: string
  cost: number
  capability: number
  endpoint_env?: string
  api_key_env?: string
  enabled: boolean
}

/** Mirrors `Model.Key()` — the value `POST /api/models/toggle` expects. */
export function modelKey(model: RoutingModel): string {
  return model.api_key_env ? `${model.id}|${model.api_key_env}` : model.id
}

// ---------------------------------------------------------------------------
// Decoding
// ---------------------------------------------------------------------------

function coerceTimestamp(value: unknown): number {
  if (typeof value === 'number') return value
  if (typeof value === 'string') {
    const parsed = Date.parse(value)
    if (!Number.isNaN(parsed)) return parsed
  }
  return Date.now()
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

/**
 * Decode a raw `/ws` frame. Returns null for anything that is not a
 * well-formed event rather than throwing — a single malformed frame must never
 * tear down the stream.
 */
export function decodeSocketEvent(raw: string): RuntimeEvent | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return null
  }
  if (!isRecord(parsed)) return null

  const type = parsed.Type ?? parsed.type
  if (typeof type !== 'string') return null

  const id = parsed.ID ?? parsed.id
  return {
    id: typeof id === 'number' ? id : 0,
    type,
    timestamp: coerceTimestamp(parsed.Timestamp ?? parsed.timestamp),
    source: String(parsed.Source ?? parsed.source ?? ''),
    sessionId: String(parsed.SessionID ?? parsed.sessionId ?? ''),
    payload: parsed.Payload ?? parsed.payload ?? null,
  }
}

/**
 * Decode one line of `logs/runtime.log`. Only `"msg":"Runtime Event"` lines
 * carry bus events; infrastructure lines (`Model routed`, cooldowns, retry
 * attempts) have no `event` key and are returned as null.
 */
export function decodeLogLine(raw: string): RuntimeEvent | null {
  const line = raw.trim()
  if (!line) return null

  let parsed: unknown
  try {
    parsed = JSON.parse(line)
  } catch {
    return null
  }
  if (!isRecord(parsed)) return null

  const type = parsed.event
  if (typeof type !== 'string') return null

  const id = parsed.id
  return {
    id: typeof id === 'number' ? id : 0,
    type,
    timestamp: coerceTimestamp(parsed.time),
    source: String(parsed.component ?? ''),
    sessionId: String(parsed.session ?? ''),
    payload: parsed.payload ?? null,
  }
}

export function isEventOfType<P>(
  event: RuntimeEvent,
  type: EventType,
): event is RuntimeEvent<P> {
  return event.type === type
}
