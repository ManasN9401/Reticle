import path from 'node:path'
import { BrowserWindow, app, dialog, ipcMain, shell } from 'electron'
import { IPC } from '../src/shared/ipc'
import type {
  ConnectRequest,
  ConnectionState,
  ForgeOutputChunk,
  ForgeStartRequest,
  ForgeState,
  HitlResolveRequest,
  LogBatch,
  LogQuery,
  NativeAction,
  OutboundCommand,
  ProjectionPush,
  ResolvedTheme,
  SettingsPatch,
  StudioSettings,
  WindowState,
} from '../src/shared/ipc'
import { installMenu } from './menu'
import { findRepoRoot } from './paths'
import { SettingsStore } from './settings'
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
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    void shell.openExternal(url)
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

function registerIpc(): void {
  ipcMain.on(IPC.windowMinimize, () => mainWindow?.minimize())
  ipcMain.on(IPC.windowMaximize, () => {
    if (!mainWindow) return
    if (mainWindow.isMaximized()) mainWindow.unmaximize()
    else mainWindow.maximize()
  })
  ipcMain.on(IPC.windowClose, () => mainWindow?.close())
  ipcMain.handle(IPC.windowState, () => windowState())

  ipcMain.handle(IPC.connectionGet, () => client.getState())
  ipcMain.handle(IPC.connectionConnect, (_e, request: ConnectRequest) => {
    settings.patch({ connection: { host: request.host, port: request.port } })
    rest.setTarget(request.host, request.port)
    return client.connect(request.host, request.port)
  })
  ipcMain.handle(IPC.connectionDisconnect, () => client.disconnect())
  ipcMain.handle(IPC.connectionSend, (_e, command: OutboundCommand) => client.send(command))

  ipcMain.handle(IPC.forgeGet, () => forge.getState())
  ipcMain.handle(IPC.forgeStart, (_e, request: ForgeStartRequest) => {
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
  ipcMain.handle(IPC.forgeStop, () => forge.stop())

  ipcMain.handle(IPC.projectionSnapshot, () => store.getProjection())
  ipcMain.handle(IPC.projectionClear, () => store.clear())
  ipcMain.handle(IPC.projectionEvents, () => store.getEvents())

  ipcMain.handle(IPC.logsQuery, (_e, query: LogQuery) => store.queryLogs(query))
  ipcMain.handle(IPC.logsScope, (_e, scope: { execId?: string; nodeId?: string }) => {
    store.setScope(scope ?? {})
  })

  ipcMain.handle(IPC.hitlRead, (_e, target: string) => readCheckpoint(target))
  ipcMain.handle(IPC.hitlResolve, (_e, request: HitlResolveRequest) =>
    resolveCheckpoint(request),
  )

  ipcMain.handle(IPC.apiModels, () => rest.models())
  ipcMain.handle(IPC.apiModelToggle, (_e, modelKey: string) => rest.toggleModel(modelKey))
  ipcMain.handle(IPC.apiOutputs, (_e, execId: string) => rest.outputs(execId))
  ipcMain.handle(IPC.apiUpload, (_e, paths: string[]) => rest.upload(paths ?? []))

  ipcMain.handle(IPC.workspaceAgents, (_e, execId?: string) => workspace.agents(execId))
  ipcMain.handle(IPC.workspaceTree, (_e, target?: string) => workspace.tree(target))
  ipcMain.handle(IPC.workspaceRead, (_e, target: string) => workspace.read(target))
  ipcMain.handle(IPC.workspaceReveal, (_e, target: string) => {
    shell.showItemInFolder(target)
  })
  ipcMain.handle(IPC.workspaceOpenExternal, (_e, url: string) => {
    // Only ever hand http(s) to the OS handler.
    if (/^https?:\/\//i.test(url)) void shell.openExternal(url)
  })
  ipcMain.handle(IPC.workspacePickDirectory, async () => {
    if (!mainWindow) return null
    const result = await dialog.showOpenDialog(mainWindow, {
      properties: ['openDirectory'],
    })
    return result.canceled ? null : (result.filePaths[0] ?? null)
  })
  ipcMain.handle(IPC.workspacePickFiles, async () => {
    if (!mainWindow) return []
    const result = await dialog.showOpenDialog(mainWindow, {
      properties: ['openFile', 'multiSelections'],
    })
    return result.canceled ? [] : result.filePaths
  })

  ipcMain.handle(IPC.envList, () => listKeys())
  ipcMain.handle(IPC.envReveal, (_e, name: string) => revealKey(name))
  ipcMain.handle(IPC.envSet, (_e, name: string, value: string) => setKey(name, value))
  ipcMain.handle(IPC.envRemove, (_e, name: string) => removeKey(name))
  ipcMain.handle(IPC.envPath, () => envFilePath())

  ipcMain.handle(IPC.themeBackground, (_e, theme: ResolvedTheme) => {
    mainWindow?.setBackgroundColor(groundColor(theme))
  })

  ipcMain.handle(IPC.nativeAction, (_e, action: NativeAction) => {
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

  ipcMain.handle(IPC.settingsGet, () => settings.get())
  ipcMain.handle(IPC.settingsPatch, (_e, patch: SettingsPatch): StudioSettings => {
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
})
