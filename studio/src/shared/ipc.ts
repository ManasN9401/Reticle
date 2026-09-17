/**
 * The complete IPC contract between main and renderer.
 *
 * Single source of truth: `preload.ts` exposes exactly these, and the renderer
 * may not reach the OS, the filesystem, or the orchestrator by any other route.
 * Channels prefixed `push:` flow main → renderer; everything else is invoke.
 */

import type { RuntimeEvent, RoutingModel, WaitlistItem } from './events'
import type { ProjectionState } from './projection'

export const IPC = {
  // --- window ------------------------------------------------------------
  windowMinimize: 'window:minimize',
  windowMaximize: 'window:maximize',
  windowClose: 'window:close',
  windowState: 'window:state',
  pushWindowState: 'push:window-state',

  // --- orchestrator connection -------------------------------------------
  connectionGet: 'connection:get',
  connectionConnect: 'connection:connect',
  connectionDisconnect: 'connection:disconnect',
  connectionSend: 'connection:send',
  pushConnectionState: 'push:connection-state',

  // --- managed forge.exe process ------------------------------------------
  forgeStart: 'forge:start',
  forgeStop: 'forge:stop',
  forgeGet: 'forge:get',
  pushForgeState: 'push:forge-state',
  pushForgeOutput: 'push:forge-output',

  // --- projection ----------------------------------------------------------
  projectionSnapshot: 'projection:snapshot',
  projectionClear: 'projection:clear',
  projectionEvents: 'projection:events',
  pushProjection: 'push:projection',

  // --- logs ----------------------------------------------------------------
  logsQuery: 'logs:query',
  logsScope: 'logs:scope',
  pushLogs: 'push:logs',

  // --- human-in-the-loop checkpoints ---------------------------------------
  hitlRead: 'hitl:read',
  hitlResolve: 'hitl:resolve',

  // --- REST passthrough (main owns the base URL) ---------------------------
  apiModels: 'api:models',
  apiModelToggle: 'api:model-toggle',
  apiOutputs: 'api:outputs',
  apiUpload: 'api:upload',

  // --- workspace / filesystem ----------------------------------------------
  workspaceAgents: 'workspace:agents',
  workspaceSummary: 'workspace:summary',
  workspaceTree: 'workspace:tree',
  workspaceRead: 'workspace:read',
  workspaceReveal: 'workspace:reveal',
  workspaceOpenExternal: 'workspace:open-external',
  workspacePickDirectory: 'workspace:pick-directory',
  workspacePickFiles: 'workspace:pick-files',

  // --- API keys (.env) -------------------------------------------------------
  envList: 'env:list',
  envReveal: 'env:reveal',
  envSet: 'env:set',
  envRemove: 'env:remove',
  envPath: 'env:path',

  // --- settings -------------------------------------------------------------
  settingsGet: 'settings:get',
  settingsPatch: 'settings:patch',
  pushSettings: 'push:settings',

  // --- native menu ----------------------------------------------------------
  pushCommand: 'push:command',
  nativeAction: 'app:native-action',
  themeBackground: 'app:theme-background',
} as const

export type IpcChannel = (typeof IPC)[keyof typeof IPC]

// ---------------------------------------------------------------------------
// Payloads
// ---------------------------------------------------------------------------

/** Narrowed here rather than using NodeJS.Platform: this type crosses into the
 *  renderer, which has no @types/node. */
export type Platform =
  | 'aix'
  | 'android'
  | 'darwin'
  | 'freebsd'
  | 'haiku'
  | 'linux'
  | 'openbsd'
  | 'sunos'
  | 'win32'
  | 'cygwin'
  | 'netbsd'

export interface WindowState {
  maximized: boolean
  focused: boolean
  platform: Platform
}

export type ConnectionPhase =
  | 'idle'
  | 'connecting'
  | 'connected'
  | 'reconnecting'
  | 'error'

export interface ConnectionState {
  phase: ConnectionPhase
  host: string
  port: number
  /** Present when phase is 'error'. */
  message?: string
  connectedAt?: number
  attempt: number
  /** Events per second over the last sampling window. */
  eventRate: number
}

export interface ConnectRequest {
  host: string
  port: number
}

/**
 * The runtime whitelists inbound commands to exactly these two
 * (runtime/telemetry/server.go:250). Anything else is silently dropped by the
 * server, so the type refuses to express it.
 */
export type OutboundCommand =
  | {
      action: 'enqueue'
      prompt: string
      group?: string
      mode?: 'parallel' | 'sequential'
      ide_context?: string
      effort?: string
      agent_complexity?: number
      attachments?: WaitlistItem['attachments']
    }
  | { action: 'remove'; id: string }
  | { action: 'kill'; id: string }
  | { action: 'pause'; id: string }
  | { action: 'resume'; id: string }
  | { 
      action: 'update_settings'
      num_ctx?: number
      max_tokens?: number
      temperature?: number
      use_bayesian_routing?: boolean
    }

export type ForgePhase = 'stopped' | 'starting' | 'running' | 'exited' | 'error'

export interface ForgeState {
  phase: ForgePhase
  pid?: number
  exitCode?: number | null
  /** Resolved absolute path of the binary that was launched. */
  binaryPath?: string
  /** Resolved cwd — must be <repo>/cmd/forge or the runtime resolves paths wrongly. */
  cwd?: string
  args?: string[]
  startedAt?: number
  message?: string
}

export interface ForgeStartRequest {
  native?: boolean
  port: number
  /** Concurrent workflows (-batch). */
  batch?: number
  /** Per-node retry budget (-retries). */
  retries?: number
  isolated?: boolean
  fresh?: boolean
  allModels?: boolean
  /** Initial prompt; forge auto-enqueues it when non-empty. */
  prompt?: string
  /** Existing workspace to resume from (-workspace). */
  workspace?: string
}

export interface ForgeOutputChunk {
  stream: 'stdout' | 'stderr'
  line: string
  at: number
}

/** A coalesced push of new events plus the state they produce. */
export interface ProjectionPush {
  state: ProjectionState
  /** Only the events since the last push, for the timeline strip. */
  events: RuntimeEvent[]
}

export interface LogRecord {
  seq: number
  at: number
  level: 'info' | 'warn' | 'error'
  execId?: string
  nodeId?: string
  agentId?: string
  message: string
  /** Bus event type when the line came from the stream rather than a worker. */
  eventType?: string
  isLlm: boolean
}

export interface LogQuery {
  execId?: string
  nodeId?: string
  /** Case-insensitive substring match over the message. */
  search?: string
  levels?: LogRecord['level'][]
  limit?: number
  /** Return records with seq strictly greater than this. */
  after?: number
}

export interface LogBatch {
  records: LogRecord[]
  /** True when older records were evicted from the ring buffer. */
  truncated: boolean
}

export interface HitlCheckpoint {
  path: string
  exists: boolean
  /** Full markdown body, for rendering the plan under review. */
  content: string
  status: 'PENDING' | 'APPROVED' | 'REJECTED' | 'UNKNOWN'
  feedback: string
}

export interface HitlResolveRequest {
  path: string
  decision: 'APPROVED' | 'REJECTED'
  feedback?: string
}

export interface OutputFile {
  path: string
  content: string
}

export interface AgentCard {
  id: string
  name?: string
  description?: string
  version?: string
  runtime?: string
  entrypoint?: string
  skills: string[]
  memory: string[]
  capabilities: string[]
  inputs: string[]
  outputs: string[]
  /** Absolute path of the manifest this was parsed from. */
  manifestPath: string
  /** Session-generated agents differ from the built-in compiler catalog. */
  origin: 'builtin' | 'session'
  /** A provisioned venv exists under .reticle/envs/<id>. */
  hasEnv: boolean
}

/** Providers the router knows how to route to. */
export type KeyProvider = 'openrouter' | 'groq' | 'gemini' | 'other'

export interface EnvKeyEntry {
  /** The environment variable name, e.g. OPENROUTER_API_KEY_2. */
  name: string
  provider: KeyProvider
  /** Which slot in the provider's rotation this is. */
  slotLabel?: string
  /**
   * False for variables the router never reads — the key-slot names are
   * hardcoded (routing/models.go:100, :194, :255).
   */
  known: boolean
  present: boolean
  /** Obfuscated for display; the plaintext requires an explicit reveal. */
  masked: string
  length: number
}

export interface TreeEntry {
  name: string
  path: string
  isDirectory: boolean
  size?: number
  modifiedAt?: number
}

export interface WorkspaceSummary {
  execId?: string
  rootPath: string
  fileCount: number
  directoryCount: number
  totalBytes: number
  latestModifiedAt?: number
  recentFiles: TreeEntry[]
  /** True when the bounded scan stopped before visiting the whole tree. */
  truncated: boolean
}

export interface ReadFileResult {
  path: string
  content: string
  truncated: boolean
  size: number
}

export type ThemePreference = 'system' | 'dark' | 'light'
export type ResolvedTheme = 'dark' | 'light'

/**
 * Node map presentation.
 *  - detailed: full card — agent, node id, model, artifacts, retries, duration
 *  - compact:  one line — status, agent, duration. For large graphs.
 *  - hex:      hexagon echoing the embedded telemetry star map. Densest.
 */
export type NodeStyle = 'detailed' | 'compact' | 'hex'

export interface StudioSettings {
  connection: {
    host: string
    port: number
    autoConnect: boolean
  }
  forge: {
    /** Defaults to <repo>/cmd/forge/forge.exe. */
    binaryPath: string
    /** Must be <repo>/cmd/forge. */
    cwd: string
    batch: number
    retries: number
    isolated: boolean
    allModels: boolean
    native: boolean
    workspace?: string
  }
  appearance: {
    /** 'system' follows the OS; the shell resolves it before it reaches the DOM. */
    theme: ThemePreference
    density: 'comfortable' | 'compact'
    reduceMotion: boolean
    /** How nodes are drawn in the node map. */
    nodeStyle: NodeStyle
  }
  logs: {
    /** Ring buffer size in the main process. */
    bufferSize: number
    followTail: boolean
  }
}

export type SettingsPatch = {
  [K in keyof StudioSettings]?: Partial<StudioSettings[K]>
}

/**
 * Actions that only the main process can perform — clipboard and undo stack on
 * the focused webContents, zoom, fullscreen, devtools, window lifecycle.
 *
 * These exist because the in-window menu bar replaces Electron's native menu on
 * Windows and Linux, and with it the built-in `role` handlers that would
 * otherwise implement Edit and View.
 */
export type NativeAction =
  | 'undo'
  | 'redo'
  | 'cut'
  | 'copy'
  | 'paste'
  | 'selectAll'
  | 'zoomIn'
  | 'zoomOut'
  | 'zoomReset'
  | 'toggleFullScreen'
  | 'toggleDevTools'
  | 'reload'
  | 'quit'

/** Commands the menu, palette and keybindings dispatch into the renderer. */
export type CommandId =
  | 'view.runs'
  | 'view.graph'
  | 'view.agents'
  | 'view.artifacts'
  | 'view.explorer'
  | 'view.settings'
  | 'panel.toggle'
  | 'panel.activity'
  | 'panel.logs'
  | 'panel.terminal'
  | 'panel.preview'
  | 'sidebar.toggle'
  | 'palette.open'
  | 'run.new'
  | 'run.stopForge'
  | 'run.startForge'
  | 'connection.connect'
  | 'connection.disconnect'
  | 'graph.relayout'
  | 'graph.fit'
  | 'logs.clear'
  | 'panel.problems'
  | 'workspace.reveal'
  | 'help.docs'
  | 'help.about'
  | 'view.zoomIn'
  | 'view.zoomOut'
  | 'view.zoomReset'
  | 'view.fullScreen'
  | 'view.devTools'
  | 'view.reload'
  | 'app.quit'
  | 'theme.cycle'
  | 'theme.dark'
  | 'theme.light'
  | 'theme.system'
  | 'graph.styleDetailed'
  | 'graph.styleCompact'
  | 'graph.styleHex'

export interface ApiResult<T> {
  ok: boolean
  data?: T
  error?: string
}

export type ModelsResult = ApiResult<RoutingModel[]>

// ---------------------------------------------------------------------------
// The bridge surface exposed on `window.reticle`
// ---------------------------------------------------------------------------

/** Unsubscribe handle returned by every `on*` subscription. */
export type Unsubscribe = () => void

export interface ReticleBridge {
  readonly version: string

  window: {
    minimize(): void
    maximize(): void
    close(): void
    getState(): Promise<WindowState>
    onState(handler: (state: WindowState) => void): Unsubscribe
  }

  connection: {
    get(): Promise<ConnectionState>
    connect(request: ConnectRequest): Promise<ConnectionState>
    disconnect(): Promise<ConnectionState>
    /** Resolves false when the socket is not open. */
    send(command: OutboundCommand): Promise<boolean>
    onState(handler: (state: ConnectionState) => void): Unsubscribe
  }

  forge: {
    get(): Promise<ForgeState>
    start(request: ForgeStartRequest): Promise<ForgeState>
    stop(): Promise<ForgeState>
    onState(handler: (state: ForgeState) => void): Unsubscribe
    onOutput(handler: (chunk: ForgeOutputChunk) => void): Unsubscribe
  }

  projection: {
    snapshot(): Promise<ProjectionState>
    clear(): Promise<ProjectionState>
    /** Raw buffered events, for the timeline scrubber's replay. */
    events(): Promise<RuntimeEvent[]>
    onPush(handler: (push: ProjectionPush) => void): Unsubscribe
  }

  logs: {
    query(query: LogQuery): Promise<LogBatch>
    /** Narrow the live push stream; omit both fields for everything. */
    scope(scope: { execId?: string; nodeId?: string }): Promise<void>
    onBatch(handler: (batch: LogBatch) => void): Unsubscribe
  }

  hitl: {
    read(path: string): Promise<HitlCheckpoint>
    resolve(request: HitlResolveRequest): Promise<ApiResult<HitlCheckpoint>>
  }

  api: {
    models(): Promise<ModelsResult>
    toggleModel(modelKey: string): Promise<ApiResult<void>>
    outputs(execId: string): Promise<ApiResult<OutputFile[]>>
    upload(paths: string[]): Promise<ApiResult<WaitlistItem['attachments']>>
  }

  workspace: {
    agents(execId?: string): Promise<ApiResult<AgentCard[]>>
    summary(execId?: string): Promise<ApiResult<WorkspaceSummary>>
    tree(path?: string): Promise<ApiResult<TreeEntry[]>>
    read(path: string): Promise<ApiResult<ReadFileResult>>
    reveal(path: string): Promise<void>
    openExternal(url: string): Promise<void>
    pickDirectory(): Promise<string | null>
    pickFiles(): Promise<string[]>
  }

  /**
   * API keys, stored in the repo-root `.env`. Values are masked by default and
   * only leave the main process through `reveal`.
   */
  keys: {
    list(): Promise<ApiResult<EnvKeyEntry[]>>
    reveal(name: string): Promise<ApiResult<string>>
    set(name: string, value: string): Promise<ApiResult<void>>
    remove(name: string): Promise<ApiResult<void>>
    path(): Promise<string | null>
  }

  settings: {
    get(): Promise<StudioSettings>
    patch(patch: SettingsPatch): Promise<StudioSettings>
    onChange(handler: (settings: StudioSettings) => void): Unsubscribe
  }

  /** Actions only the main process can perform (clipboard, zoom, devtools, quit). */
  native(action: NativeAction): Promise<void>

  /**
   * Keep the native window background in step with the resolved theme, so a
   * reload or relaunch does not flash the wrong colour before first paint.
   */
  setThemeBackground(theme: ResolvedTheme): Promise<void>

  onCommand(handler: (command: CommandId) => void): Unsubscribe
}

declare global {
  interface Window {
    /**
     * Undefined only if the preload failed to load — the shell surfaces that
     * as a hard error rather than letting controls silently no-op.
     */
    reticle?: ReticleBridge
  }
}
