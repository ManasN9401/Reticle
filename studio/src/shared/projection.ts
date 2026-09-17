/** Pure projection shared by Electron and the renderer.
 * Reconnect restores authoritative node/run states via WorkflowSnapshot.
 * The event ring buffer is bounded and in memory; it is not a durable event log.
 */

import type {
  Artifact,
  RuntimeEvent,
  WaitlistPayload,
  WireEdge,
  WorkerFailedPayload,
  WorkerFailureReason,
  WorkerLogPayload,
  WorkflowStartedPayload,
} from './events'
import {
  TASK_ID_SEPARATOR as TASK_SEPARATOR,
  identify,
  identifyExecution,
  indicatesMockFallback,
  parseCheckpointPath,
  parseUiState,
} from './ids'

/**
 * Four states are real (runtime/agent/workflow_execution.go:5-12). `waiting` is
 * synthesized by Studio from the `[UI_STATE: ...]` stderr sentinel — the engine
 * still considers such a node `running`, but showing it as running is a lie:
 * it is blocked on a human.
 */
export type NodeStatus = 'pending' | 'running' | 'done' | 'failed' | 'waiting' | 'blocked' | 'interrupted'

export type RunStatus = 'pending' | 'running' | 'completed' | 'failed' | 'paused' | 'cancelled' | 'interrupted'

export type WaitingKind = 'human' | 'comfy'

export interface ArtifactRef {
  id: string
  name?: string
  type?: string
  producer?: string
  version?: number
  createdAt?: number
  /** Payloads are stripped before they reach the renderer; this records what was there. */
  hasData: boolean
  dataSize?: number
}

export interface NodeFailure {
  reason: WorkerFailureReason | string
  exitCode?: number
  stderr?: string
}

export interface WaitingState {
  kind: WaitingKind
  since: number
  /** Absolute path to the approval markdown, scraped from the worker's own log. */
  checkpointPath?: string
}

export interface RunNode {
  execId: string
  nodeId: string
  taskId: string
  /** Semantic agent id, when known. */
  agentId?: string
  /** What to render: the agent id if we have it, else the raw node id. */
  label: string
  status: NodeStatus
  model?: string
  startedAt?: number
  endedAt?: number
  durationMs?: number
  /** Derived from repeated WorkerStarted on the same task id. */
  attempts: number
  artifacts: ArtifactRef[]
  failure?: NodeFailure
  waiting?: WaitingState
  /** RFC-026 §8: mock fallbacks must be visibly distinguishable from real work. */
  mocked: boolean
  logCount: number
}

export interface RunEdge {
  from: string
  to: string
}

export interface Run {
  execId: string
  workflowId?: string
  status: RunStatus
  nodes: Record<string, RunNode>
  edges: RunEdge[]
  startedAt?: number
  endedAt?: number
  failureNodeId?: string
  failureReason?: string
}

export interface ProjectionState {
  runs: Record<string, Run>
  /** Insertion order, oldest first. */
  runOrder: string[]
  waitlist: WaitlistPayload | null
  lastEventId: number
  eventCount: number
  sessionId?: string
}

export function createProjection(): ProjectionState {
  return {
    runs: {},
    runOrder: [],
    waitlist: null,
    lastEventId: 0,
    eventCount: 0,
  }
}

function createRun(execId: string): Run {
  return { execId, status: 'pending', nodes: {}, edges: [] }
}

function createNode(execId: string, nodeId: string, taskId: string): RunNode {
  return {
    execId,
    nodeId,
    taskId,
    label: nodeId,
    status: 'pending',
    attempts: 0,
    artifacts: [],
    mocked: false,
    logCount: 0,
  }
}

function toArtifactRef(artifact: Artifact): ArtifactRef {
  const data = artifact.data
  const hasData = data !== undefined && data !== null
  let dataSize: number | undefined
  if (typeof data === 'string') dataSize = data.length
  else if (hasData) {
    try {
      dataSize = JSON.stringify(data).length
    } catch {
      dataSize = undefined
    }
  }
  return {
    id: artifact.id,
    name: artifact.name,
    type: artifact.type,
    producer: artifact.producer,
    version: artifact.version,
    createdAt: artifact.created_at ? Date.parse(artifact.created_at) : undefined,
    hasData,
    dataSize,
  }
}

/**
 * Batch context implementing copy-on-write.
 *
 * `applyEvents` never mutates the state handed to it — it clones each run and
 * node the batch actually touches, exactly once, then mutates only those
 * clones. Callers get the same guarantees as a naive immutable reducer without
 * paying to rebuild untouched runs on every one of tens of thousands of events.
 */
interface Batch {
  runs: Record<string, Run>
  runOrder: string[]
  clonedRuns: Set<string>
  clonedNodes: Set<string>
}

function draftRun(batch: Batch, execId: string): Run {
  const existing = batch.runs[execId]
  if (!existing) {
    const created = createRun(execId)
    batch.runs[execId] = created
    batch.runOrder = [...batch.runOrder, execId]
    batch.clonedRuns.add(execId)
    return created
  }
  if (batch.clonedRuns.has(execId)) return existing
  const clone: Run = { ...existing, nodes: { ...existing.nodes }, edges: existing.edges }
  batch.runs[execId] = clone
  batch.clonedRuns.add(execId)
  return clone
}

function draftNode(
  batch: Batch,
  run: Run,
  nodeId: string,
  taskId: string,
): RunNode {
  const key = `${run.execId}|${nodeId}`
  const existing = run.nodes[nodeId]
  if (!existing) {
    const created = createNode(run.execId, nodeId, taskId)
    run.nodes[nodeId] = created
    batch.clonedNodes.add(key)
    return created
  }
  if (batch.clonedNodes.has(key)) return existing
  const clone: RunNode = { ...existing, artifacts: existing.artifacts }
  run.nodes[nodeId] = clone
  batch.clonedNodes.add(key)
  return clone
}

/** Keep the display label pinned to the semantic agent id once we learn it. */
function setAgent(node: RunNode, agentId: string | undefined): void {
  if (!agentId) return
  node.agentId = agentId
  node.label = agentId
}

function applyToBatch(batch: Batch, state: ProjectionState, event: RuntimeEvent): void {
  const payload = event.payload as Record<string, unknown> | null

  switch (event.type) {
    case 'RuntimeOverloaded': {
      for (const id of batch.runOrder) {
        const run=draftRun(batch,id)
        if (['pending','running','paused'].includes(run.status)) {
          run.status='failed';run.failureReason='Runtime event capacity exceeded; restart required'
          for (const node of Object.values(run.nodes)) {
            if (node.status!=='done') draftNode(batch,run,node.nodeId,node.taskId).status='failed'
          }
        }
      }
      if (state.waitlist) state.waitlist={...state.waitlist,runningWorkers:0,items:(state.waitlist.items??[]).map(item=>['PENDING','RUNNING','PAUSED'].includes(item.status)?{...item,status:'FAILED'}:item)}
      return
    }
    case 'RuntimePersistenceFailed': {
      for (const id of batch.runOrder) {
        const run=draftRun(batch,id)
        if (['pending','running','paused'].includes(run.status)) {
          run.status='interrupted';run.failureReason='Runtime persistence failed; reconcile state and restart'
          for (const node of Object.values(run.nodes)) {
            const draft=draftNode(batch,run,node.nodeId,node.taskId)
            if (node.status==='running' || node.status==='waiting') draft.status='interrupted'
            else if (node.status==='pending') draft.status='blocked'
          }
        }
      }
      return
    }
    case 'WorkflowSnapshot':
    case 'WorkflowStarted': {
      const p = (payload ?? {}) as WorkflowStartedPayload
      const execId = p.exec_id ?? identifyExecution(payload)
      if (!execId) return
      const run = draftRun(batch, execId)
      if (event.type === 'WorkflowSnapshot' && payload) {
        const statuses: string[] = ['running','paused','completed','failed','cancelled','interrupted']
        if (statuses.includes(String(payload.status))) run.status = payload.status as RunStatus
        const states = payload.nodes
        if (states && typeof states === 'object') {
          for (const [id, status] of Object.entries(states)) {
            if (['pending','running','done','failed','blocked','interrupted'].includes(String(status))) {
              draftNode(batch,run,id,`${execId}|${id}`).status = status as NodeStatus
            }
          }
        }
      }
      run.workflowId = p.workflow_id ?? run.workflowId
      if (run.status === 'pending') {
        run.status = 'running'
        run.startedAt = event.timestamp
      }
      const edges: WireEdge[] = Array.isArray(p.edges) ? p.edges : []
      if (edges.length > 0) {
        run.edges = edges
          .map((edge) => ({ from: edge.from ?? edge.From ?? '', to: edge.to ?? edge.To ?? '' }))
          .filter((edge) => edge.from.length > 0 && edge.to.length > 0)
        // Seed every node the topology mentions so the graph is complete
        // before any of them has produced an event.
        for (const edge of run.edges) {
          for (const nodeId of [edge.from, edge.to]) {
            if (nodeId) draftNode(batch, run, nodeId, `${execId}|${nodeId}`)
          }
        }
      }
      return
    }

    case 'NodeReady':
    case 'TaskCreated': {
      const identity = identify(payload)
      if (!identity) return
      const run = draftRun(batch, identity.execId)
      const node = draftNode(batch, run, identity.nodeId, identity.taskId)
      setAgent(node, identity.agentId)
      return
    }

    case 'TaskDispatched': {
      const identity = identify(payload)
      if (!identity) return
      const run = draftRun(batch, identity.execId)
      const node = draftNode(batch, run, identity.nodeId, identity.taskId)
      setAgent(node, identity.agentId)
      const model = payload?.llm_model
      if (typeof model === 'string' && model) node.model = model
      return
    }

    case 'WorkerStarted': {
      const identity = identify(payload)
      if (!identity) return
      const run = draftRun(batch, identity.execId)
      const node = draftNode(batch, run, identity.nodeId, identity.taskId)
      setAgent(node, identity.agentId)
      node.status = 'running'
      node.startedAt = event.timestamp
      node.endedAt = undefined
      node.durationMs = undefined
      node.failure = undefined
      node.waiting = undefined
      // A second start for the same task is a retry, not a new node.
      node.attempts += 1
      if (run.status === 'pending') {
        run.status = 'running'
        run.startedAt = run.startedAt ?? event.timestamp
      }
      return
    }

    case 'WorkerCompleted': {
      const identity = identify(payload)
      if (!identity) return
      const run = draftRun(batch, identity.execId)
      const node = draftNode(batch, run, identity.nodeId, identity.taskId)
      setAgent(node, identity.agentId)
      node.status = 'done'
      node.endedAt = event.timestamp
      node.waiting = undefined
      // The backend sends no duration; it only exists as this delta.
      if (node.startedAt !== undefined) {
        node.durationMs = Math.max(0, event.timestamp - node.startedAt)
      }
      return
    }

    case 'WorkerFailed':
    case 'TaskFailed': {
      const identity = identify(payload)
      if (!identity) return
      const run = draftRun(batch, identity.execId)
      const node = draftNode(batch, run, identity.nodeId, identity.taskId)
      setAgent(node, identity.agentId)
      node.status = 'failed'
      node.endedAt = event.timestamp
      node.waiting = undefined
      if (node.startedAt !== undefined) {
        node.durationMs = Math.max(0, event.timestamp - node.startedAt)
      }
      const p = (payload ?? {}) as WorkerFailedPayload
      if (p.reason || p.exit_code !== undefined || p.stderr) {
        node.failure = {
          reason: p.reason ?? 'exit_non_zero',
          exitCode: p.exit_code,
          stderr: p.stderr,
        }
      }
      return
    }

    case 'WorkerLog': {
      const identity = identify(payload)
      if (!identity) return
      const p = (payload ?? {}) as WorkerLogPayload
      const line = p.log
      const signal = parseUiState(line)
      const checkpoint = parseCheckpointPath(line)
      const mocked = indicatesMockFallback(line)

      // The overwhelming majority of WorkerLog events change nothing about the
      // projection. Only clone when there is something to record.
      if (!signal && !checkpoint && !mocked) {
        const run = batch.runs[identity.execId]
        const node = run?.nodes[identity.nodeId]
        if (!run || !node) return
        const drafted = draftNode(batch, draftRun(batch, identity.execId), identity.nodeId, identity.taskId)
        drafted.logCount += 1
        return
      }

      const run = draftRun(batch, identity.execId)
      const node = draftNode(batch, run, identity.nodeId, identity.taskId)
      setAgent(node, identity.agentId)
      node.logCount += 1
      if (mocked) node.mocked = true
      if (signal === 'RESUMED') {
        node.waiting = undefined
        if (node.status === 'waiting') node.status = 'running'
      } else if (signal === 'WAITING_HUMAN' || signal === 'WAITING_COMFY') {
        node.status = 'waiting'
        node.waiting = {
          kind: signal === 'WAITING_HUMAN' ? 'human' : 'comfy',
          since: event.timestamp,
          checkpointPath: node.waiting?.checkpointPath,
        }
      }
      if (checkpoint) {
        node.waiting = {
          kind: node.waiting?.kind ?? 'human',
          since: node.waiting?.since ?? event.timestamp,
          checkpointPath: checkpoint,
        }
        if (node.status !== 'waiting') node.status = 'waiting'
      }
      return
    }

    case 'ArtifactsProduced':
    case 'ArtifactStored':
    case 'ArtifactVersionCreated': {
      const artifact = payload as unknown as Artifact | null
      if (!artifact || typeof artifact.id !== 'string') return
      const execId = artifact.execution ?? identifyExecution(payload)
      if (!execId) return

      // Resolving which node produced this is the one place the wire format
      // will actively mislead you. `task` carries the real composite id, but
      // it is often absent — and falling back to `producer` invents a node
      // named after the *agent*, which then sits in the graph forever as a
      // duplicate stuck in `pending`. So: use `task` when present, otherwise
      // attach to an existing node that already claims this agent, and if
      // neither resolves, drop the artifact rather than fabricate a node.
      const existing = batch.runs[execId]
      let nodeId: string | undefined
      if (artifact.task) {
        const split = artifact.task.split(TASK_SEPARATOR)
        nodeId = split.length > 1 ? split[1] : undefined
      }
      if (!nodeId && artifact.producer && existing) {
        const match = Object.values(existing.nodes).find(
          (candidate) =>
            candidate.agentId === artifact.producer ||
            candidate.nodeId === artifact.producer,
        )
        nodeId = match?.nodeId
      }
      if (!nodeId) return

      const run = draftRun(batch, execId)
      const node = draftNode(batch, run, nodeId, artifact.task ?? `${execId}|${nodeId}`)
      setAgent(node, artifact.producer)
      const ref = toArtifactRef(artifact)
      const index = node.artifacts.findIndex(
        (a) => a.id === ref.id && a.version === ref.version,
      )
      node.artifacts =
        index === -1
          ? [...node.artifacts, ref]
          : node.artifacts.map((a, i) => (i === index ? ref : a))
      return
    }

    case 'ExecutionPaused':
    case 'ExecutionResumed':
    case 'ExecutionKilled': {
      const execId=identifyExecution(payload);if(!execId)return
      const run=draftRun(batch,execId)
      if(['completed','failed','cancelled'].includes(run.status))return
      run.status=event.type==='ExecutionPaused'?'paused':event.type==='ExecutionKilled'?'cancelled':'running'
      if(run.status==='cancelled')run.endedAt=event.timestamp
      return
    }
    case 'WorkflowCompleted': {
      const execId = identifyExecution(payload)
      if (!execId) return
      const run = draftRun(batch, execId)
      run.status = 'completed'
      run.endedAt = event.timestamp
      return
    }

    case 'WorkflowFailed': {
      const execId = identifyExecution(payload)
      if (!execId) return
      const run = draftRun(batch, execId)
      run.status = 'failed'
      run.endedAt = event.timestamp
      const nodeId = payload?.node_id
      if (typeof nodeId === 'string') run.failureNodeId = nodeId
      const reason = payload?.reason
      if (typeof reason === 'string') run.failureReason = reason
      return
    }

    case 'WaitlistUpdated': {
      const p = payload as unknown as WaitlistPayload | null
      if (!p) return
      // The replay path omits lockedKeys; don't let it erase what we know.
      state.waitlist = {
        items: p.items ?? [],
        maxWorkers: p.maxWorkers ?? 0,
        runningWorkers: p.runningWorkers ?? 0,
        lockedKeys: p.lockedKeys ?? state.waitlist?.lockedKeys ?? null,
      }
      return
    }

    default:
      return
  }
}

/**
 * Fold a batch of events into new state. The input state is never mutated.
 */
export function applyEvents(
  state: ProjectionState,
  events: readonly RuntimeEvent[],
): ProjectionState {
  if (events.length === 0) return state
  // A batch may straddle reconnects; discard the previous session's prefix.
  const lastSession = events.at(-1)?.sessionId
  if (lastSession) {
    const boundary = events.findLastIndex(event => !!event.sessionId && event.sessionId !== lastSession)
    if (boundary >= 0) return applyEvents(createProjection(), events.slice(boundary + 1))
  }
  const newestSession=events.at(-1)?.sessionId
  if(newestSession && state.sessionId && newestSession!==state.sessionId)state=createProjection()

  const next: ProjectionState = {
    ...state,
    waitlist: state.waitlist,
  }
  const batch: Batch = {
    runs: { ...state.runs },
    runOrder: state.runOrder,
    clonedRuns: new Set(),
    clonedNodes: new Set(),
  }

  for (const event of events) {
    applyToBatch(batch, next, event)
    if (event.id > next.lastEventId) next.lastEventId = event.id
    if (event.sessionId) next.sessionId = event.sessionId
  }

  next.runs = batch.runs
  next.runOrder = batch.runOrder
  next.eventCount = state.eventCount + events.length
  return next
}

export function applyEvent(
  state: ProjectionState,
  event: RuntimeEvent,
): ProjectionState {
  return applyEvents(state, [event])
}

/**
 * Rebuild state from scratch up to (and including) `maxEventId`. Backs the
 * timeline scrubber — RFC-022 §9's time-travel debugging.
 */
export function replayTo(
  events: readonly RuntimeEvent[],
  maxEventId: number,
): ProjectionState {
  const upTo: RuntimeEvent[] = []
  for (const event of events) {
    if (event.id > maxEventId) continue
    upTo.push(event)
  }
  return applyEvents(createProjection(), upTo)
}

// ---------------------------------------------------------------------------
// Selectors
// ---------------------------------------------------------------------------

export interface RunTotals {
  total: number
  pending: number
  running: number
  done: number
  failed: number
  waiting: number
  blocked: number
  interrupted: number
}

export function runTotals(run: Run | undefined): RunTotals {
  const totals: RunTotals = {
    total: 0,
    pending: 0,
    running: 0,
    done: 0,
    failed: 0,
    waiting: 0,
    blocked: 0,
    interrupted: 0,
  }
  if (!run) return totals
  for (const node of Object.values(run.nodes)) {
    totals.total += 1
    totals[node.status] += 1
  }
  return totals
}

/** Nodes currently blocked on a human decision, newest first. */
export function pendingApprovals(state: ProjectionState): RunNode[] {
  const result: RunNode[] = []
  for (const execId of state.runOrder) {
    const run = state.runs[execId]
    if (!run) continue
    for (const node of Object.values(run.nodes)) {
      if (node.status === 'waiting' && node.waiting?.kind === 'human') {
        result.push(node)
      }
    }
  }
  return result.sort((a, b) => (b.waiting?.since ?? 0) - (a.waiting?.since ?? 0))
}

export function latestRun(state: ProjectionState): Run | undefined {
  for (let i = state.runOrder.length - 1; i >= 0; i -= 1) {
    const run = state.runs[state.runOrder[i]]
    if (run) return run
  }
  return undefined
}
