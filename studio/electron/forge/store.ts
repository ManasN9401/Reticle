import { EventEmitter } from 'node:events'
import type { Artifact, RuntimeEvent } from '../../src/shared/events'
import { identify, isLlmLog } from '../../src/shared/ids'
import {
  applyEvents,
  createProjection,
  type ProjectionState,
} from '../../src/shared/projection'
import type {
  LogBatch,
  LogQuery,
  LogRecord,
  ProjectionPush,
} from '../../src/shared/ipc'

/**
 * Coalescing windows.
 *
 * State was flushed every frame, which meant a busy run pushed ~60 store updates
 * a second into the renderer and left the node map with no headroom for zoom or
 * pan. Node status simply does not change at 60fps; 100ms is imperceptible here
 * and cuts the churn roughly sixfold. The status bar's event-rate readout is
 * sampled independently in ForgeClient and is unaffected.
 */
const STATE_FLUSH_MS = 100
const LOG_FLUSH_MS = 100

/**
 * `Artifact.Data` is broadcast unstripped over the socket and can be megabytes.
 * The renderer never needs the bytes to draw a graph, so we keep a short
 * preview and drop the rest before anything crosses the IPC boundary.
 */
const ARTIFACT_PREVIEW_CHARS = 2_000

const ARTIFACT_EVENTS = new Set([
  'ArtifactsProduced',
  'ArtifactStored',
  'ArtifactVersionCreated',
])

/**
 * Bus events worth surfacing in the global log. `WorkerLog` is deliberately
 * excluded here and handled separately: it is 77% of all event volume, and
 * mixing it into the structural stream makes that stream unreadable.
 */
const STRUCTURAL_LOG_EVENTS = new Set([
  'RuntimeOverloaded',
  'WorkflowStarted',
  'WorkflowCompleted',
  'WorkflowFailed',
  'NodeReady',
  'TaskDispatched',
  'WorkerStarted',
  'WorkerCompleted',
  'WorkerFailed',
  'TaskFailed',
  'ArtifactsProduced',
  'RuntimeStarted',
  'RuntimeShutdown',
])

const ERROR_PATTERN = /\b(error|exception|traceback|failed|fatal)\b/i
const WARN_PATTERN = /\b(warn|warning|retry|retrying|rate.?limit|deprecated)\b/i

function classify(message: string): LogRecord['level'] {
  if (ERROR_PATTERN.test(message)) return 'error'
  if (WARN_PATTERN.test(message)) return 'warn'
  return 'info'
}

/** Bounded FIFO. Oldest entries are dropped once capacity is reached. */
class RingBuffer<T> {
  private items: T[] = []
  private evicted = false
  private capacity: number

  constructor(capacity: number) {
    this.capacity = capacity
  }

  push(item: T): void {
    this.items.push(item)
    if (this.items.length > this.capacity) {
      this.items.splice(0, this.items.length - this.capacity)
      this.evicted = true
    }
  }

  toArray(): T[] {
    return this.items
  }

  get hasEvicted(): boolean {
    return this.evicted
  }

  setCapacity(capacity: number): void {
    this.capacity = capacity
    if (this.items.length > capacity) {
      this.items.splice(0, this.items.length - capacity)
      this.evicted = true
    }
  }

  clear(): void {
    this.items = []
    this.evicted = false
  }
}

/** Bounded in-memory history survives renderer reloads. WorkflowSnapshot restores
 * current run/node states on reconnect; historical timings require retained events.
 */
export class EventStore extends EventEmitter {
  private events: RingBuffer<RuntimeEvent>
  private logs: RingBuffer<LogRecord>
  private projection: ProjectionState = createProjection()

  private pendingEvents: RuntimeEvent[] = []
  private pendingLogs: LogRecord[] = []
  private stateTimer: NodeJS.Timeout | null = null
  private logTimer: NodeJS.Timeout | null = null
  private logSeq = 0

  /** Narrows which log records are pushed live; queries are unaffected. */
  private scope: { execId?: string; nodeId?: string } = {}

  constructor(bufferSize = 50_000) {
    super()
    this.events = new RingBuffer(bufferSize)
    this.logs = new RingBuffer(bufferSize)
  }

  setBufferSize(size: number): void {
    this.events.setCapacity(size)
    this.logs.setCapacity(size)
  }

  setScope(scope: { execId?: string; nodeId?: string }): void {
    this.scope = scope
  }

  getProjection(): ProjectionState {
    return this.projection
  }

  getEvents(): RuntimeEvent[] {
    return this.events.toArray()
  }

  ingest(raw: RuntimeEvent): void {
    const event = this.stripHeavyPayload(raw)
    this.events.push(event)
    this.pendingEvents.push(event)

    const record = this.toLogRecord(event)
    if (record) {
      this.logs.push(record)
      this.pendingLogs.push(record)
    }

    this.scheduleFlush()
  }

  clear(): ProjectionState {
    this.events.clear()
    this.logs.clear()
    this.pendingEvents = []
    this.pendingLogs = []
    this.projection = createProjection()
    this.emit('projection', {
      state: this.projection,
      events: [],
    } satisfies ProjectionPush)
    return this.projection
  }

  queryLogs(query: LogQuery): LogBatch {
    const limit = query.limit ?? 2_000
    const search = query.search?.toLowerCase()
    const levels = query.levels && query.levels.length > 0 ? new Set(query.levels) : null

    const matched: LogRecord[] = []
    const all = this.logs.toArray()
    // Walk backwards so `limit` keeps the newest records, not the oldest.
    for (let i = all.length - 1; i >= 0 && matched.length < limit; i -= 1) {
      const record = all[i]
      if (query.after !== undefined && record.seq <= query.after) break
      if (query.execId && record.execId !== query.execId) continue
      if (query.nodeId && record.nodeId !== query.nodeId) continue
      if (levels && !levels.has(record.level)) continue
      if (search && !record.message.toLowerCase().includes(search)) continue
      matched.push(record)
    }
    matched.reverse()

    return { records: matched, truncated: this.logs.hasEvicted }
  }

  private stripHeavyPayload(event: RuntimeEvent): RuntimeEvent {
    if (!ARTIFACT_EVENTS.has(event.type)) return event
    const payload = event.payload as Artifact | null
    if (!payload || typeof payload !== 'object') return event
    if (payload.data === undefined || payload.data === null) return event

    const data = payload.data
    let preview: string
    if (typeof data === 'string') {
      preview = data.slice(0, ARTIFACT_PREVIEW_CHARS)
    } else {
      try {
        preview = JSON.stringify(data).slice(0, ARTIFACT_PREVIEW_CHARS)
      } catch {
        preview = '[unserializable]'
      }
    }

    return {
      ...event,
      payload: { ...payload, data: preview },
    }
  }

  private toLogRecord(event: RuntimeEvent): LogRecord | null {
    const identity = identify(event.payload)
    const base = {
      seq: (this.logSeq += 1),
      at: event.timestamp,
      execId: identity?.execId,
      nodeId: identity?.nodeId,
      agentId: identity?.agentId,
    }

    if (event.type === 'WorkerLog') {
      const payload = event.payload as { log?: string } | null
      const message = payload?.log
      if (!message) return null
      return {
        ...base,
        level: classify(message),
        message,
        isLlm: isLlmLog(message),
      }
    }

    if (event.type === 'WorkerFailed') {
      const payload = event.payload as
        | { reason?: string; exit_code?: number; stderr?: string }
        | null
      const reason = payload?.reason ?? 'failed'
      const code = payload?.exit_code
      const detail = payload?.stderr?.trim()
      return {
        ...base,
        level: 'error',
        message: `worker failed: ${reason}${code === undefined ? '' : ` (exit ${code})`}${
          detail ? ` — ${detail.split('\n')[0]}` : ''
        }`,
        eventType: event.type,
        isLlm: false,
      }
    }

    if (!STRUCTURAL_LOG_EVENTS.has(event.type)) return null

    return {
      ...base,
      level: event.type.endsWith('Failed') ? 'error' : 'info',
      message: describe(event),
      eventType: event.type,
      isLlm: false,
    }
  }

  private scheduleFlush(): void {
    if (!this.stateTimer) {
      this.stateTimer = setTimeout(() => {
        this.stateTimer = null
        this.flushState()
      }, STATE_FLUSH_MS)
    }
    if (!this.logTimer) {
      this.logTimer = setTimeout(() => {
        this.logTimer = null
        this.flushLogs()
      }, LOG_FLUSH_MS)
    }
  }

  private flushState(): void {
    if (this.pendingEvents.length === 0) return
    const batch = this.pendingEvents
    this.pendingEvents = []
    this.projection = applyEvents(this.projection, batch)
    this.emit('projection', {
      state: this.projection,
      events: batch,
    } satisfies ProjectionPush)
  }

  private flushLogs(): void {
    if (this.pendingLogs.length === 0) return
    const batch = this.pendingLogs
    this.pendingLogs = []
    const scoped = batch.filter((record) => {
      if (this.scope.execId && record.execId !== this.scope.execId) return false
      if (this.scope.nodeId && record.nodeId !== this.scope.nodeId) return false
      return true
    })
    if (scoped.length === 0) return
    this.emit('logs', {
      records: scoped,
      truncated: this.logs.hasEvicted,
    } satisfies LogBatch)
  }

  dispose(): void {
    if (this.stateTimer) clearTimeout(this.stateTimer)
    if (this.logTimer) clearTimeout(this.logTimer)
    this.removeAllListeners()
  }
}

/** Human-readable one-liners for structural events. */
function describe(event: RuntimeEvent): string {
  const p = (event.payload ?? {}) as Record<string, unknown>
  switch (event.type) {
    case 'WorkflowStarted':
      return `workflow started: ${String(p.workflow_id ?? '')}`
    case 'WorkflowCompleted':
      return `workflow completed: ${String(p.workflow ?? '')}`
    case 'WorkflowFailed':
      return `workflow failed at ${String(p.node_id ?? '?')}: ${String(p.reason ?? '')}`
    case 'NodeReady':
      return `node ready: ${String(p.node_id ?? '')}`
    case 'TaskDispatched':
      return `dispatched${p.llm_model ? ` on ${String(p.llm_model)}` : ''}`
    case 'WorkerStarted':
      return 'worker started'
    case 'WorkerCompleted':
      return 'worker completed'
    case 'TaskFailed':
      return `task failed: ${String(p.node_id ?? '')}`
    case 'ArtifactsProduced':
      return `artifact produced: ${String(p.name ?? p.id ?? '')}`
    case 'RuntimeStarted':
      return 'runtime started'
    case 'RuntimeShutdown':
      return 'runtime shutdown'
    default:
      return event.type
  }
}
