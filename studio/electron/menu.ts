import { Menu, shell, type BrowserWindow, type MenuItemConstructorOptions } from 'electron'
import { IPC } from '../src/shared/ipc'
import type { CommandId } from '../src/shared/ipc'

/**
 * Application menu.
 *
 * The window is frameless, and a frameless window on Windows and Linux does not
 * render a native menu bar at all — the menu would exist but be invisible, with
 * only its accelerators reachable. So on those platforms the menu is removed
 * entirely and `src/app/MenuBar.tsx` draws it inside the title bar instead,
 * with the renderer owning the keybindings.
 *
 * macOS is different: its menu bar lives at the top of the screen rather than in
 * the window, so it works fine with a frameless window and users expect it to be
 * there. It keeps the native menu, and the in-window bar hides itself.
 */
export function installMenu(getWindow: () => BrowserWindow | null): void {
  if (process.platform !== 'darwin') {
    Menu.setApplicationMenu(null)
    return
  }

  const send = (command: CommandId) => () => {
    getWindow()?.webContents.send(IPC.pushCommand, command)
  }

  const template: MenuItemConstructorOptions[] = [
    { role: 'appMenu' },
    {
      label: 'File',
      submenu: [
        { label: 'New Run…', accelerator: 'CmdOrCtrl+N', click: send('run.new') },
        { type: 'separator' },
        { label: 'Settings', accelerator: 'CmdOrCtrl+,', click: send('view.settings') },
        {
          label: 'Reveal Workspace in Finder',
          click: send('workspace.reveal'),
        },
        { type: 'separator' },
        { role: 'close' },
      ],
    },
    {
      label: 'Edit',
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
      label: 'View',
      submenu: [
        {
          label: 'Command Palette…',
          accelerator: 'CmdOrCtrl+Shift+P',
          click: send('palette.open'),
        },
        { type: 'separator' },
        { label: 'Runs', accelerator: 'CmdOrCtrl+1', click: send('view.runs') },
        { label: 'Node Map', accelerator: 'CmdOrCtrl+2', click: send('view.graph') },
        { label: 'Agents', accelerator: 'CmdOrCtrl+3', click: send('view.agents') },
        { label: 'Artifacts', accelerator: 'CmdOrCtrl+4', click: send('view.artifacts') },
        { label: 'Explorer', accelerator: 'CmdOrCtrl+5', click: send('view.explorer') },
        { type: 'separator' },
        {
          label: 'Toggle Sidebar',
          accelerator: 'CmdOrCtrl+B',
          click: send('sidebar.toggle'),
        },
        { label: 'Toggle Panel', accelerator: 'CmdOrCtrl+J', click: send('panel.toggle') },
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
      label: 'Run',
      submenu: [
        { label: 'Start Forge', accelerator: 'F5', click: send('run.startForge') },
        { label: 'Stop Forge', accelerator: 'Shift+F5', click: send('run.stopForge') },
        { type: 'separator' },
        { label: 'Connect', click: send('connection.connect') },
        { label: 'Disconnect', click: send('connection.disconnect') },
        { type: 'separator' },
        {
          label: 'Re-layout Node Map',
          accelerator: 'CmdOrCtrl+Alt+L',
          click: send('graph.relayout'),
        },
        {
          label: 'Fit Node Map',
          accelerator: 'CmdOrCtrl+Alt+F',
          click: send('graph.fit'),
        },
      ],
    },
    {
      label: 'Terminal',
      submenu: [
        {
          label: 'Show Terminal',
          accelerator: 'CmdOrCtrl+`',
          click: send('panel.terminal'),
        },
        { label: 'Show Logs', accelerator: 'CmdOrCtrl+Shift+U', click: send('panel.logs') },
        { label: 'Show Problems', click: send('panel.problems') },
        { label: 'Show Preview', click: send('panel.preview') },
        { type: 'separator' },
        { label: 'Clear Logs', click: send('logs.clear') },
      ],
    },
    {
      label: 'Help',
      submenu: [
        {
          label: 'Reticle Documentation',
          click: () => {
            void shell.openExternal('https://github.com/')
          },
        },
        { label: 'About Reticle Studio', click: send('help.about') },
      ],
    },
  ]

  Menu.setApplicationMenu(Menu.buildFromTemplate(template))
}
