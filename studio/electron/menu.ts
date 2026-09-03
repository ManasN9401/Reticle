import { Menu, shell, type BrowserWindow, type MenuItemConstructorOptions } from 'electron'
import { IPC } from '../src/shared/ipc'
import type { CommandId } from '../src/shared/ipc'

/**
 * Native application menu.
 *
 * Every entry dispatches a `CommandId` into the renderer, which resolves it
 * through the same command registry the palette and keybindings use — so an
 * action has exactly one implementation regardless of how it was triggered.
 */
export function installMenu(getWindow: () => BrowserWindow | null): void {
  const send = (command: CommandId) => () => {
    getWindow()?.webContents.send(IPC.pushCommand, command)
  }

  const isMac = process.platform === 'darwin'

  const template: MenuItemConstructorOptions[] = [
    ...(isMac
      ? ([{ role: 'appMenu' }] satisfies MenuItemConstructorOptions[])
      : []),
    {
      label: '&File',
      submenu: [
        { label: 'New Run…', accelerator: 'CmdOrCtrl+N', click: send('run.new') },
        { type: 'separator' },
        { label: 'Settings', accelerator: 'CmdOrCtrl+,', click: send('view.settings') },
        { type: 'separator' },
        isMac ? { role: 'close' } : { role: 'quit' },
      ],
    },
    {
      label: '&Edit',
      submenu: [
        { role: 'undo' },
        { role: 'redo' },
        { type: 'separator' },
        { role: 'cut' },
        { role: 'copy' },
        { role: 'paste' },
        { role: 'selectAll' },
      ],
    },
    {
      label: '&View',
      submenu: [
        { label: 'Command Palette…', accelerator: 'CmdOrCtrl+Shift+P', click: send('palette.open') },
        { type: 'separator' },
        { label: 'Runs', accelerator: 'CmdOrCtrl+1', click: send('view.runs') },
        { label: 'Graph', accelerator: 'CmdOrCtrl+2', click: send('view.graph') },
        { label: 'Agents', accelerator: 'CmdOrCtrl+3', click: send('view.agents') },
        { label: 'Artifacts', accelerator: 'CmdOrCtrl+4', click: send('view.artifacts') },
        { label: 'Explorer', accelerator: 'CmdOrCtrl+5', click: send('view.explorer') },
        { type: 'separator' },
        { label: 'Toggle Sidebar', accelerator: 'CmdOrCtrl+B', click: send('sidebar.toggle') },
        { label: 'Toggle Panel', accelerator: 'CmdOrCtrl+J', click: send('panel.toggle') },
        { label: 'Logs', accelerator: 'CmdOrCtrl+Shift+U', click: send('panel.logs') },
        { type: 'separator' },
        { role: 'resetZoom' },
        { role: 'zoomIn' },
        { role: 'zoomOut' },
        { type: 'separator' },
        { role: 'togglefullscreen' },
        { role: 'toggleDevTools' },
      ],
    },
    {
      label: '&Run',
      submenu: [
        { label: 'Start Forge', accelerator: 'F5', click: send('run.startForge') },
        { label: 'Stop Forge', accelerator: 'Shift+F5', click: send('run.stopForge') },
        { type: 'separator' },
        { label: 'Connect', click: send('connection.connect') },
        { label: 'Disconnect', click: send('connection.disconnect') },
        { type: 'separator' },
        { label: 'Re-layout Graph', accelerator: 'CmdOrCtrl+Alt+L', click: send('graph.relayout') },
        { label: 'Fit Graph', accelerator: 'CmdOrCtrl+Alt+F', click: send('graph.fit') },
        { type: 'separator' },
        { label: 'Clear Logs', click: send('logs.clear') },
      ],
    },
    {
      label: '&Help',
      submenu: [
        {
          label: 'Reticle Documentation',
          click: () => {
            void shell.openExternal('https://github.com/')
          },
        },
      ],
    },
  ]

  Menu.setApplicationMenu(Menu.buildFromTemplate(template))
}
