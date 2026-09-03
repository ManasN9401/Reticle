/**
 * The renderer's only route to the OS, the filesystem, and the orchestrator.
 *
 * Bundled to CommonJS on purpose: Electron loads an ESM preload only when it is
 * named `.mjs`, and doing so would force `sandbox: false`. CommonJS lets the
 * renderer stay fully sandboxed. Nothing here needs Node beyond `ipcRenderer` —
 * every privileged operation lives in the main process.
 */

import { contextBridge, ipcRenderer } from 'electron'
import type { IpcRendererEvent } from 'electron'
import { IPC } from '../src/shared/ipc'
import type {
  AgentCard,
  ApiResult,
  CommandId,
  ConnectRequest,
  ConnectionState,
  EnvKeyEntry,
  ForgeOutputChunk,
  ForgeStartRequest,
  ForgeState,
  HitlCheckpoint,
  HitlResolveRequest,
  LogBatch,
  LogQuery,
  ModelsResult,
  NativeAction,
  OutboundCommand,
  OutputFile,
  ProjectionPush,
  ReadFileResult,
  ResolvedTheme,
  ReticleBridge,
  SettingsPatch,
  StudioSettings,
  TreeEntry,
  Unsubscribe,
  WindowState,
} from '../src/shared/ipc'
import type { RuntimeEvent, WaitlistItem } from '../src/shared/events'
import type { ProjectionState } from '../src/shared/projection'

function subscribe<T>(channel: string, handler: (value: T) => void): Unsubscribe {
  const listener = (_event: IpcRendererEvent, value: T) => handler(value)
  ipcRenderer.on(channel, listener)
  return () => {
    ipcRenderer.removeListener(channel, listener)
  }
}

const bridge: ReticleBridge = {
  version: '0.1.0',

  window: {
    minimize: () => ipcRenderer.send(IPC.windowMinimize),
    maximize: () => ipcRenderer.send(IPC.windowMaximize),
    close: () => ipcRenderer.send(IPC.windowClose),
    getState: () => ipcRenderer.invoke(IPC.windowState) as Promise<WindowState>,
    onState: (handler) => subscribe<WindowState>(IPC.pushWindowState, handler),
  },

  connection: {
    get: () => ipcRenderer.invoke(IPC.connectionGet) as Promise<ConnectionState>,
    connect: (request: ConnectRequest) =>
      ipcRenderer.invoke(IPC.connectionConnect, request) as Promise<ConnectionState>,
    disconnect: () =>
      ipcRenderer.invoke(IPC.connectionDisconnect) as Promise<ConnectionState>,
    send: (command: OutboundCommand) =>
      ipcRenderer.invoke(IPC.connectionSend, command) as Promise<boolean>,
    onState: (handler) => subscribe<ConnectionState>(IPC.pushConnectionState, handler),
  },

  forge: {
    get: () => ipcRenderer.invoke(IPC.forgeGet) as Promise<ForgeState>,
    start: (request: ForgeStartRequest) =>
      ipcRenderer.invoke(IPC.forgeStart, request) as Promise<ForgeState>,
    stop: () => ipcRenderer.invoke(IPC.forgeStop) as Promise<ForgeState>,
    onState: (handler) => subscribe<ForgeState>(IPC.pushForgeState, handler),
    onOutput: (handler) => subscribe<ForgeOutputChunk>(IPC.pushForgeOutput, handler),
  },

  projection: {
    snapshot: () =>
      ipcRenderer.invoke(IPC.projectionSnapshot) as Promise<ProjectionState>,
    clear: () => ipcRenderer.invoke(IPC.projectionClear) as Promise<ProjectionState>,
    events: () => ipcRenderer.invoke(IPC.projectionEvents) as Promise<RuntimeEvent[]>,
    onPush: (handler) => subscribe<ProjectionPush>(IPC.pushProjection, handler),
  },

  logs: {
    query: (query: LogQuery) =>
      ipcRenderer.invoke(IPC.logsQuery, query) as Promise<LogBatch>,
    scope: (scope) => ipcRenderer.invoke(IPC.logsScope, scope) as Promise<void>,
    onBatch: (handler) => subscribe<LogBatch>(IPC.pushLogs, handler),
  },

  hitl: {
    read: (path: string) =>
      ipcRenderer.invoke(IPC.hitlRead, path) as Promise<HitlCheckpoint>,
    resolve: (request: HitlResolveRequest) =>
      ipcRenderer.invoke(IPC.hitlResolve, request) as Promise<ApiResult<HitlCheckpoint>>,
  },

  api: {
    models: () => ipcRenderer.invoke(IPC.apiModels) as Promise<ModelsResult>,
    toggleModel: (modelKey: string) =>
      ipcRenderer.invoke(IPC.apiModelToggle, modelKey) as Promise<ApiResult<void>>,
    outputs: (execId: string) =>
      ipcRenderer.invoke(IPC.apiOutputs, execId) as Promise<ApiResult<OutputFile[]>>,
    upload: (paths: string[]) =>
      ipcRenderer.invoke(IPC.apiUpload, paths) as Promise<
        ApiResult<WaitlistItem['attachments']>
      >,
  },

  workspace: {
    agents: (execId?: string) =>
      ipcRenderer.invoke(IPC.workspaceAgents, execId) as Promise<ApiResult<AgentCard[]>>,
    tree: (path?: string) =>
      ipcRenderer.invoke(IPC.workspaceTree, path) as Promise<ApiResult<TreeEntry[]>>,
    read: (path: string) =>
      ipcRenderer.invoke(IPC.workspaceRead, path) as Promise<ApiResult<ReadFileResult>>,
    reveal: (path: string) => ipcRenderer.invoke(IPC.workspaceReveal, path) as Promise<void>,
    openExternal: (url: string) =>
      ipcRenderer.invoke(IPC.workspaceOpenExternal, url) as Promise<void>,
    pickDirectory: () =>
      ipcRenderer.invoke(IPC.workspacePickDirectory) as Promise<string | null>,
    pickFiles: () => ipcRenderer.invoke(IPC.workspacePickFiles) as Promise<string[]>,
  },

  keys: {
    list: () => ipcRenderer.invoke(IPC.envList) as Promise<ApiResult<EnvKeyEntry[]>>,
    reveal: (name: string) =>
      ipcRenderer.invoke(IPC.envReveal, name) as Promise<ApiResult<string>>,
    set: (name: string, value: string) =>
      ipcRenderer.invoke(IPC.envSet, name, value) as Promise<ApiResult<void>>,
    remove: (name: string) =>
      ipcRenderer.invoke(IPC.envRemove, name) as Promise<ApiResult<void>>,
    path: () => ipcRenderer.invoke(IPC.envPath) as Promise<string | null>,
  },

  settings: {
    get: () => ipcRenderer.invoke(IPC.settingsGet) as Promise<StudioSettings>,
    patch: (patch: SettingsPatch) =>
      ipcRenderer.invoke(IPC.settingsPatch, patch) as Promise<StudioSettings>,
    onChange: (handler) => subscribe<StudioSettings>(IPC.pushSettings, handler),
  },

  native: (action: NativeAction) =>
    ipcRenderer.invoke(IPC.nativeAction, action) as Promise<void>,

  setThemeBackground: (theme: ResolvedTheme) =>
    ipcRenderer.invoke(IPC.themeBackground, theme) as Promise<void>,

  onCommand: (handler: (command: CommandId) => void) =>
    subscribe<CommandId>(IPC.pushCommand, handler),
}

contextBridge.exposeInMainWorld('reticle', bridge)
