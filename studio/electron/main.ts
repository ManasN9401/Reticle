import path from 'node:path'
import { promises as fs } from 'node:fs'
import { BrowserWindow, app, clipboard, dialog, ipcMain, shell } from 'electron'
import { IPC } from '../src/shared/ipc'
import type {
  ColorScheme,
  ColorToken,
  ConnectRequest,
  ConnectionState,
  FileExportResult,
  ForgeOutputChunk,
  ForgeStartRequest,
  ForgeState,
  HitlResolveRequest,
  LogBatch,
  LogExportFormat,
  LogExportRequest,
  LogExportResult,
  LogRecord,
  LogQuery,
  NativeAction,
  OutboundCommand,
  ProfileImportResult,
  ProjectionPush,
  ResolvedTheme,
  SettingsPatch,
  SettingsProfile,
  StudioSettings,
  ThemeExportResult,
  ThemeImportResult,
  WindowState,
  TerminalStartRequest,
} from '../src/shared/ipc'
import { COLOR_TOKENS, isSafeColorValue } from '../src/shared/ipc'
import { installMenu } from './menu'
import { findRepoRoot, setRepoRoot } from './paths'
import { TerminalManager } from './terminal/manager'
import { SettingsStore, validateColorSchemes, validateNodeAppearance } from './settings'
import { ForgeClient } from './forge/client'
import { ForgeProcess } from './forge/process'
import { ForgeRest } from './forge/rest'
import { EventStore } from './forge/store'
import { envFilePath, listKeys, removeKey, revealKey, setKey } from './env/keys'
import { readCheckpoint, resolveCheckpoint } from './hitl/checkpoints'
import { WorkspaceReader } from './workspace/reader'

let mainWindow: BrowserWindow | null = null

const settings = new SettingsStore()
const client = new ForgeClient()
const forge = new ForgeProcess()
const rest = new ForgeRest()
const store = new EventStore(settings.get().logs.bufferSize)
const workspace = new WorkspaceReader(findRepoRoot())
const terminals = new TerminalManager(
  () => findRepoRoot(),
  (id, data) => send(IPC.pushTerminalData, { id, data }),
  event => send(IPC.pushTerminalExit, event),
)

function send<T>(channel: string, payload: T): void {
  if (mainWindow && !mainWindow.isDestroyed()) {
    mainWindow.webContents.send(channel, payload)
  }
}

/** Mirrors the `--color-bg-0` token for each theme. */
function groundColor(theme: ResolvedTheme): string {
  return theme === 'light' ? '#eef0f4' : '#0b0c0e'
}

function windowState(): WindowState {
  return {
    maximized: mainWindow?.isMaximized() ?? false,
    focused: mainWindow?.isFocused() ?? false,
    platform: process.platform,
  }
}

function formatLogRecords(records: LogRecord[], format: LogExportFormat): string {
  if (format === 'json') return JSON.stringify(records, null, 2)
  if (format === 'csv') {
    const escape = (value: unknown) => {
      const text = value === undefined || value === null ? '' : String(value)
      return /[",\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text
    }
    const rows = records.map((r) =>
      [r.seq, new Date(r.at).toISOString(), r.level, r.execId, r.nodeId, r.agentId, r.eventType, r.isLlm, r.message]
        .map(escape)
        .join(','),
    )
    return ['seq,at,level,execId,nodeId,agentId,eventType,isLlm,message', ...rows].join('\n')
  }
  return records
    .map((r) => {
      const scope = [r.agentId, r.nodeId].filter(Boolean).join(' · ')
      return `${new Date(r.at).toISOString()} [${r.level}]${scope ? ` ${scope}` : ''} ${r.message}`
    })
    .join('\n')
}

/**
 * Covers only what the in-memory ring buffer still holds, not the full
 * on-disk logs/runtime.log — there is no historical-log REST endpoint yet.
 */
async function exportLogs(request: LogExportRequest): Promise<LogExportResult> {
  const batch = store.queryLogs({ ...request.query, limit: request.query.limit ?? Infinity })
  const content = formatLogRecords(batch.records, request.format)

  if (request.destination === 'clipboard') {
    clipboard.writeText(content)
    return { ok: true, count: batch.records.length }
  }

  if (!mainWindow) return { ok: false, count: 0, error: 'Window unavailable' }
  const extension = request.format === 'json' ? 'json' : request.format === 'csv' ? 'csv' : 'log'
  const result = await dialog.showSaveDialog(mainWindow, {
    defaultPath: `reticle-logs.${extension}`,
    filters: [{ name: request.format.toUpperCase(), extensions: [extension] }],
  })
  if (result.canceled || !result.filePath) return { ok: false, count: 0, error: 'Export cancelled' }
  try {
    await fs.writeFile(result.filePath, content, 'utf8')
    return { ok: true, path: result.filePath, count: batch.records.length }
  } catch (error) {
    return { ok: false, count: 0, error: error instanceof Error ? error.message : String(error) }
  }
}

/** Sanitizes a user-given name into a safe filename stem — reused by every "export this named thing as JSON" feature. */
function safeFileStem(name: string, fallback: string): string {
  return name.replace(/[^\w.-]+/g, '-').replace(/^-+|-+$/g, '') || fallback
}

/** Shared by every "save a JSON file the user picks" export. */
async function exportJsonFile(defaultStem: string, data: unknown): Promise<FileExportResult> {
  if (!mainWindow) return { ok: false, error: 'Window unavailable' }
  const result = await dialog.showSaveDialog(mainWindow, {
    defaultPath: `${defaultStem}.json`,
    filters: [{ name: 'JSON', extensions: ['json'] }],
  })
  if (result.canceled || !result.filePath) return { ok: false, error: 'Export cancelled' }
  try {
    await fs.writeFile(result.filePath, JSON.stringify(data, null, 2), 'utf8')
    return { ok: true, path: result.filePath }
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : String(error) }
  }
}

/** Shared by every "open a file picker and parse/validate a previously exported JSON file" import. */
async function importJsonFile<T>(validate: (value: unknown) => T | null): Promise<{ ok: boolean; data?: T; error?: string }> {
  if (!mainWindow) return { ok: false, error: 'Window unavailable' }
  const result = await dialog.showOpenDialog(mainWindow, {
    properties: ['openFile'],
    filters: [{ name: 'JSON', extensions: ['json'] }],
  })
  if (result.canceled || result.filePaths.length === 0) return { ok: false, error: 'Import cancelled' }
  try {
    const raw = await fs.readFile(result.filePaths[0], 'utf8')
    const data = validate(JSON.parse(raw))
    if (!data) return { ok: false, error: 'Not a valid file' }
    return { ok: true, data }
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : String(error) }
  }
}

async function exportTheme(scheme: ColorScheme): Promise<ThemeExportResult> {
  return exportJsonFile(safeFileStem(scheme.name, 'reticle-theme'), scheme)
}

/**
 * Untrusted input: a scheme file the user picked, parsed and re-validated
 * field by field before it can ever reach `settings.json` or an injected
 * stylesheet. Unknown token keys are dropped; malformed colour values are
 * dropped individually rather than failing the whole import.
 */
function validateImportedScheme(value: unknown): Omit<ColorScheme, 'id'> | null {
  if (!value || typeof value !== 'object') return null
  const raw = value as Record<string, unknown>
  if (typeof raw.name !== 'string' || !raw.name.trim()) return null
  if (raw.base !== 'dark' && raw.base !== 'light') return null
  const tokensRaw = raw.tokens
  if (tokensRaw !== undefined && (typeof tokensRaw !== 'object' || tokensRaw === null)) return null
  const tokens: Partial<Record<ColorToken, string>> = {}
  for (const [key, val] of Object.entries((tokensRaw ?? {}) as Record<string, unknown>)) {
    if (!(COLOR_TOKENS as readonly string[]).includes(key)) continue
    if (typeof val !== 'string' || !isSafeColorValue(val)) continue
    tokens[key as ColorToken] = val.trim()
  }
  return { name: raw.name.trim().slice(0, 80), base: raw.base, tokens }
}

async function importTheme(): Promise<ThemeImportResult> {
  const result = await importJsonFile(validateImportedScheme)
  return result.ok && result.data
    ? { ok: true, scheme: result.data }
    : { ok: false, error: result.error }
}

async function exportProfile(profile: SettingsProfile): Promise<FileExportResult> {
  return exportJsonFile(safeFileStem(profile.name, 'reticle-profile'), profile)
}

/**
 * Untrusted input: a profile file the user picked. Re-validates its embedded
 * colour schemes and node appearance the same way settings.json itself is
 * validated; appearance/connection are structurally trusted (same depth the
 * top-level settings already apply to those fields).
 */
function validateImportedProfile(value: unknown): Omit<SettingsProfile, 'id'> | null {
  if (!value || typeof value !== 'object') return null
  const raw = value as Record<string, unknown>
  if (typeof raw.name !== 'string' || !raw.name.trim()) return null
  const snapshot = raw.snapshot
  if (!snapshot || typeof snapshot !== 'object') return null
  const s = snapshot as Record<string, unknown>
  try {
    validateNodeAppearance(s.nodeAppearance as StudioSettings['nodeAppearance'])
    validateColorSchemes(s.colorSchemes as StudioSettings['colorSchemes'])
  } catch {
    return null
  }
  if (!s.appearance || typeof s.appearance !== 'object') return null
  if (!s.connection || typeof s.connection !== 'object') return null
  return {
    name: raw.name.trim().slice(0, 80),
    snapshot: s as SettingsProfile['snapshot'],
  }
}

async function importProfile(): Promise<ProfileImportResult> {
  const result = await importJsonFile(validateImportedProfile)
  return result.ok && result.data
    ? { ok: true, profile: result.data }
    : { ok: false, error: result.error }
}

function createWindow(): void {
  mainWindow = new BrowserWindow({
    width: 1480,
    height: 940,
    minWidth: 1024,
    minHeight: 640,
    show: false,
    // Match the --color-bg-0 token of the persisted theme, so there is no
    // flash of the wrong colour before the renderer paints.
    backgroundColor: groundColor(
      settings.get().appearance.theme === 'light' ? 'light' : 'dark',
    ),
    titleBarStyle: 'hidden',
    frame: false,
    webPreferences: {
      // The renderer gets no Node and no direct OS access; everything
      // privileged goes through the preload's narrow IPC surface.
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
      // vite-plugin-electron emits this as CommonJS `preload.js`. Keeping it
      // CommonJS is deliberate: an ESM preload must be named `.mjs` and would
      // force `sandbox: false`.
      preload: path.join(__dirname, 'preload.js'),
    },
  })

  mainWindow.once('ready-to-show', () => mainWindow?.show())

  const pushWindowState = () => send(IPC.pushWindowState, windowState())
  mainWindow.on('maximize', pushWindowState)
  mainWindow.on('unmaximize', pushWindowState)
  mainWindow.on('focus', pushWindowState)
  mainWindow.on('blur', pushWindowState)

  // Never let the app itself navigate away or spawn unmanaged windows.
  mainWindow.webContents.on('will-navigate', event => event.preventDefault())
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    if (/^https?:\/\//i.test(url)) void shell.openExternal(url)
    return { action: 'deny' }
  })

  if (process.env.VITE_DEV_SERVER_URL) {
    void mainWindow.loadURL(process.env.VITE_DEV_SERVER_URL)
  } else {
    void mainWindow.loadFile(path.join(__dirname, '../dist/index.html'))
  }

  mainWindow.on('closed', () => {
    mainWindow = null
  })
}

// ---------------------------------------------------------------------------
// Wiring: socket -> store -> renderer
// ---------------------------------------------------------------------------

client.on('event', (event) => store.ingest(event))

client.on('state', (state: ConnectionState) => {
  rest.setTarget(state.host, state.port)
  send(IPC.pushConnectionState, state)
})

store.on('projection', (push: ProjectionPush) => send(IPC.pushProjection, push))
store.on('logs', (batch: LogBatch) => send(IPC.pushLogs, batch))

forge.on('state', (state: ForgeState) => send(IPC.pushForgeState, state))
forge.on('output', (chunk: ForgeOutputChunk) => send(IPC.pushForgeOutput, chunk))

// ---------------------------------------------------------------------------
// IPC handlers
// ---------------------------------------------------------------------------

const handle: typeof ipcMain.handle = (channel, listener) => {
  ipcMain.handle(channel, (event, ...args) => {
    if (!mainWindow || event.sender !== mainWindow.webContents || event.senderFrame !== mainWindow.webContents.mainFrame) throw new Error('Untrusted IPC sender')
    return listener(event, ...args)
  })
}
function registerIpc(): void {
  ipcMain.on(IPC.windowMinimize, () => mainWindow?.minimize())
  ipcMain.on(IPC.windowMaximize, () => {
    if (!mainWindow) return
    if (mainWindow.isMaximized()) mainWindow.unmaximize()
    else mainWindow.maximize()
  })
  ipcMain.on(IPC.windowClose, () => mainWindow?.close())
  handle(IPC.windowState, () => windowState())

  handle(IPC.connectionGet, () => client.getState())
  handle(IPC.connectionConnect, (_e, request: ConnectRequest) => {
    settings.patch({ connection: { host: request.host, port: request.port } })
    rest.setTarget(request.host, request.port)
    return client.connect(request.host, request.port)
  })
  handle(IPC.connectionDisconnect, () => client.disconnect())
  handle(IPC.connectionSend, (_e, command: OutboundCommand) => client.send(command))

  handle(IPC.forgeGet, () => forge.getState())
  handle(IPC.forgeStart, (_e, request: ForgeStartRequest) => {
    const state = forge.start(request, settings.get())
    // Give the telemetry server a moment to bind before attaching.
    if (state.phase === 'running') {
      setTimeout(() => {
        const { host } = settings.get().connection
        client.connect(host, request.port)
      }, 1_200)
    }
    return state
  })
  handle(IPC.forgeStop, () => forge.stop())

  handle(IPC.projectionSnapshot, () => store.getProjection())
  handle(IPC.projectionClear, () => store.clear())
  handle(IPC.projectionEvents, () => store.getEvents())

  handle(IPC.logsQuery, (_e, query: LogQuery) => store.queryLogs(query))
  handle(IPC.logsScope, (_e, scope: { execId?: string; nodeId?: string }) => {
    store.setScope(scope ?? {})
  })
  handle(IPC.logsExport, (_e, request: LogExportRequest) => exportLogs(request))

  handle(IPC.hitlRead, (_e, target: string) => readCheckpoint(target))
  handle(IPC.hitlResolve, (_e, request: HitlResolveRequest) =>
    resolveCheckpoint(request),
  )

  handle(IPC.apiModels, () => rest.models())
  handle(IPC.apiModelToggle, (_e, modelKey: string) => rest.toggleModel(modelKey))
  handle(IPC.apiOutputs, (_e, execId: string) => rest.outputs(execId))
  handle(IPC.apiUpload, (_e, paths: string[]) => rest.upload(paths ?? []))

  handle(IPC.workspaceAgents, (_e, execId?: string) => workspace.agents(execId))
  handle(IPC.workspaceSummary, (_e, execId?: string) => workspace.summary(execId))
  handle(IPC.workspaceTree, (_e, target?: string) => workspace.tree(target))
  handle(IPC.workspaceRead, (_e, target: string) => workspace.read(target))
  handle(IPC.workspaceReveal, (_e, target: string) => {
    shell.showItemInFolder(target)
  })
  handle(IPC.workspaceOpenExternal, (_e, url: string) => {
    // Only ever hand http(s) to the OS handler.
    if (/^https?:\/\//i.test(url)) void shell.openExternal(url)
  })
  handle(IPC.workspacePickDirectory, async () => {
    if (!mainWindow) return null
    const result = await dialog.showOpenDialog(mainWindow, {
      properties: ['openDirectory'],
    })
    return result.canceled ? null : (result.filePaths[0] ?? null)
  })
  handle(IPC.workspacePickFiles, async () => {
    if (!mainWindow) return []
    const result = await dialog.showOpenDialog(mainWindow, {
      properties: ['openFile', 'multiSelections'],
    })
    return result.canceled ? [] : result.filePaths
  })

  handle(IPC.terminalStart, (_e, request: TerminalStartRequest) =>
    terminals.start(request.cwd, request.cols, request.rows),
  )
  handle(IPC.terminalWrite, (_e, id: string, data: string) => terminals.write(id, data))
  handle(IPC.terminalResize, (_e, id: string, cols: number, rows: number) =>
    terminals.resize(id, cols, rows),
  )
  handle(IPC.terminalStop, (_e, id: string) => terminals.stop(id))

  handle(IPC.envList, () => listKeys())
  handle(IPC.envReveal, (_e, name: string) => revealKey(name))
  handle(IPC.envSet, (_e, name: string, value: string) => setKey(name, value))
  handle(IPC.envRemove, (_e, name: string) => removeKey(name))
  handle(IPC.envPath, () => envFilePath())

  handle(IPC.themeBackground, (_e, theme: ResolvedTheme) => {
    mainWindow?.setBackgroundColor(groundColor(theme))
  })

  handle(IPC.nativeAction, (_e, action: NativeAction) => {
    const contents = mainWindow?.webContents
    if (!contents) return
    switch (action) {
      // The in-window menu bar replaces Electron's native menu on Windows and
      // Linux, so the `role` handlers that would normally back Edit and View
      // have to be driven explicitly.
      case 'undo': return contents.undo()
      case 'redo': return contents.redo()
      case 'cut': return contents.cut()
      case 'copy': return contents.copy()
      case 'paste': return contents.paste()
      case 'selectAll': return contents.selectAll()
      case 'zoomIn':
        return void contents.setZoomLevel(Math.min(5, contents.getZoomLevel() + 0.5))
      case 'zoomOut':
        return void contents.setZoomLevel(Math.max(-5, contents.getZoomLevel() - 0.5))
      case 'zoomReset':
        return void contents.setZoomLevel(0)
      case 'toggleFullScreen':
        return mainWindow?.setFullScreen(!mainWindow.isFullScreen())
      case 'toggleDevTools':
        return contents.toggleDevTools()
      case 'reload':
        return contents.reload()
      case 'quit':
        return app.quit()
      default:
        return
    }
  })

  handle(IPC.themeExport, (_e, scheme: ColorScheme) => exportTheme(scheme))
  handle(IPC.themeImport, () => importTheme())

  handle(IPC.profileExport, (_e, profile: SettingsProfile) => exportProfile(profile))
  handle(IPC.profileImport, () => importProfile())

  handle(IPC.settingsGet, () => settings.get())
  handle(IPC.settingsPatch, (_e, patch: SettingsPatch): StudioSettings => {
    if(patch.forge?.cwd){setRepoRoot(path.resolve(patch.forge.cwd,'../..'));workspace.setRoot(findRepoRoot())}
    const next = settings.patch(patch)
    store.setBufferSize(next.logs.bufferSize)
    send(IPC.pushSettings, next)
    return next
  })
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// A second instance would fight over the same window and socket.
if (!app.requestSingleInstanceLock()) {
  app.quit()
} else {
  app.on('second-instance', () => {
    if (!mainWindow) return
    if (mainWindow.isMinimized()) mainWindow.restore()
    mainWindow.focus()
  })

  void app.whenReady().then(() => {
    registerIpc()
    installMenu(() => mainWindow)
    createWindow()

    const { connection } = settings.get()
    if (connection.autoConnect) {
      client.connect(connection.host, connection.port)
    }

    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) createWindow()
    })
  })
}

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit()
})

app.on('before-quit', () => {
  // Never leave an orphaned forge.exe holding the telemetry port.
  forge.dispose()
  client.dispose()
  store.dispose()
  terminals.dispose()
})
