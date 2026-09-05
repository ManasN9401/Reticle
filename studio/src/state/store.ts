import { create } from 'zustand'
import type { RuntimeEvent } from '@shared/events'
import type {
  ConnectionState,
  ForgeOutputChunk,
  ForgeState,
  LogRecord,
  StudioSettings,
  WindowState,
} from '@shared/ipc'
import {
  createProjection,
  latestRun,
  type ProjectionState,
  type Run,
  type RunNode,
} from '@shared/projection'
import { bridge } from './bridge'

/** Cap on log records held in the renderer; the main process keeps far more. */
const LOG_LIMIT = 20_000
/** Cap on timeline ticks. The scrubber only needs shape, not every event. */
const TIMELINE_LIMIT = 20_000

export interface StudioStore {
  ready: boolean
  bridgeMissing: boolean

  windowState: WindowState
  connection: ConnectionState
  forge: ForgeState
  settings: StudioSettings | null

  projection: ProjectionState
  /** Lightweight ticks backing the timeline scrubber. */
  timeline: { id: number; at: number; type: string }[]
  logs: LogRecord[]
  logsTruncated: boolean

  /** Tracks the OS colour-scheme so 'system' theme can resolve reactively. */
  systemPrefersLight: boolean

  /** Currently inspected run; null means "follow the newest". */
  selectedExecId: string | null
  selectedNodeId: string | null
  /** When set, the graph renders historical state at this event id. */
  scrubEventId: number | null

  setSystemPrefersLight(light: boolean): void
  initialize(): Promise<void>
  selectRun(execId: string | null): void
  selectNode(nodeId: string | null): void
  setScrub(eventId: number | null): void
  clearLogs(): void
}

function emptyConnection(): ConnectionState {
  return { phase: 'idle', host: '127.0.0.1', port: 8080, attempt: 0, eventRate: 0 }
}

export const useStudio = create<StudioStore>((set, get) => ({
  ready: false,
  bridgeMissing: !bridge,

  windowState: { maximized: false, focused: true, platform: 'win32' },
  connection: emptyConnection(),
  forge: { phase: 'stopped' },
  settings: null,

  projection: createProjection(),
  timeline: [],
  logs: [],
  logsTruncated: false,

  systemPrefersLight: false,

  selectedExecId: null,
  selectedNodeId: null,
  scrubEventId: null,

  setSystemPrefersLight(light) {
    if (get().systemPrefersLight !== light) set({ systemPrefersLight: light })
  },

  async initialize() {
    if (!bridge) {
      set({ ready: true, bridgeMissing: true })
      return
    }

    const [windowState, connection, forge, settings, projection, events] =
      await Promise.all([
        bridge.window.getState(),
        bridge.connection.get(),
        bridge.forge.get(),
        bridge.settings.get(),
        bridge.projection.snapshot(),
        bridge.projection.events(),
      ])

    set({
      ready: true,
      windowState,
      connection,
      forge,
      settings,
      projection,
      timeline: toTicks(events),
    })

    // Backfill the log panel from the main-process ring buffer so a renderer
    // reload does not appear to lose the run.
    const backfill = await bridge.logs.query({ limit: 4_000 })
    set({ logs: backfill.records, logsTruncated: backfill.truncated })

    bridge.window.onState((next) => set({ windowState: next }))
    bridge.connection.onState((next) => set({ connection: next }))
    bridge.forge.onState((next) => set({ forge: next }))
    bridge.settings.onChange((next) => set({ settings: next }))

    bridge.projection.onPush(({ state, events: batch }) => {
      set((prev) => ({
        projection: state,
        timeline: appendCapped(prev.timeline, toTicks(batch), TIMELINE_LIMIT),
      }))
    })

    bridge.logs.onBatch((batch) => {
      set((prev) => ({
        logs: appendCapped(prev.logs, batch.records, LOG_LIMIT),
        logsTruncated: prev.logsTruncated || batch.truncated,
      }))
    })

    // forge's own stdout/stderr is not on the event bus, so fold it into the
    // same stream the user is already reading.
    bridge.forge.onOutput((chunk: ForgeOutputChunk) => {
      set((prev) => ({
        logs: appendCapped(
          prev.logs,
          [
            {
              seq: -Date.now() - prev.logs.length,
              at: chunk.at,
              level: chunk.stream === 'stderr' ? 'warn' : 'info',
              message: chunk.line,
              agentId: 'forge',
              isLlm: false,
            },
          ],
          LOG_LIMIT,
        ),
      }))
    })

    if (!get().selectedExecId) {
      const run = latestRun(projection)
      if (run) set({ selectedExecId: run.execId })
    }
  },

  selectRun(execId) {
    set({ selectedExecId: execId, selectedNodeId: null, scrubEventId: null })
    void bridge?.logs.scope(execId ? { execId } : {})
  },

  selectNode(nodeId) {
    set({ selectedNodeId: nodeId })
  },

  setScrub(eventId) {
    set({ scrubEventId: eventId })
  },

  clearLogs() {
    set({ logs: [], logsTruncated: false })
  },
}))

function toTicks(events: readonly RuntimeEvent[]) {
  return events.map((event) => ({ id: event.id, at: event.timestamp, type: event.type }))
}

function appendCapped<T>(existing: T[], incoming: T[], limit: number): T[] {
  if (incoming.length === 0) return existing
  const next = existing.concat(incoming)
  return next.length > limit ? next.slice(next.length - limit) : next
}

// ---------------------------------------------------------------------------
// Selectors
// ---------------------------------------------------------------------------

/** The run the UI is currently showing: the explicit selection, else the newest. */
export function useActiveRun(): Run | undefined {
  return useStudio((s) => {
    if (s.selectedExecId) return s.projection.runs[s.selectedExecId]
    return latestRun(s.projection)
  })
}

export function useActiveNode(): RunNode | undefined {
  const run = useActiveRun()
  const nodeId = useStudio((s) => s.selectedNodeId)
  if (!run || !nodeId) return undefined
  return run.nodes[nodeId]
}
