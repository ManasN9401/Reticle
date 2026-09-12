import type { CommandId } from '@shared/ipc'
import {
  canStartForge,
  canStopForge,
  connect,
  disconnect,
  startForge,
  stopForge,
} from '@/state/actions'
import { bridge } from '@/state/bridge'
import { useStudio } from '@/state/store'
import { useUi } from '@/state/ui'
import { useSettingsUi } from '@/features/settings/state'
import { nextTheme } from '@/state/theme'
import type { NodeStyle, ThemePreference } from '@shared/ipc'

/**
 * The single command registry.
 *
 * The native menu, the command palette and the keyboard shortcuts all resolve
 * through here, so an action has exactly one implementation no matter how it
 * was triggered — and nothing can drift into being wired in one place but not
 * another.
 */

export interface Command {
  id: CommandId
  title: string
  section: string
  keys?: string
  run: () => void | Promise<void>
  /** Rendered but disabled when false, with `disabledReason` explaining why. */
  enabled?: () => boolean
  disabledReason?: string
}

/** Graph-view actions are registered by the graph itself while it is mounted. */
const graphHandlers: Partial<Record<CommandId, () => void>> = {}

export function registerGraphHandler(id: CommandId, handler: (() => void) | null): void {
  if (handler) graphHandlers[id] = handler
  else delete graphHandlers[id]
}

export const COMMANDS: Command[] = [
  {
    id: 'view.runs',
    title: 'Show Runs',
    section: 'View',
    keys: 'Ctrl+1',
    run: () => useUi.getState().setView('runs'),
  },
  {
    id: 'view.graph',
    title: 'Show Node Map',
    section: 'View',
    keys: 'Ctrl+2',
    run: () => useUi.getState().setView('graph'),
  },
  {
    id: 'view.agents',
    title: 'Show Agents',
    section: 'View',
    keys: 'Ctrl+3',
    run: () => useUi.getState().setView('agents'),
  },
  {
    id: 'view.artifacts',
    title: 'Show Artifacts',
    section: 'View',
    keys: 'Ctrl+4',
    run: () => useUi.getState().setView('artifacts'),
  },
  {
    id: 'view.explorer',
    title: 'Show Explorer',
    section: 'View',
    keys: 'Ctrl+5',
    run: () => useUi.getState().setView('explorer'),
  },
  {
    id: 'view.settings',
    title: 'Open Settings',
    section: 'View',
    keys: 'Ctrl+,',
    run: () => useUi.getState().setView('settings'),
  },
  {
    id: 'sidebar.toggle',
    title: 'Toggle Sidebar',
    section: 'View',
    keys: 'Ctrl+B',
    run: () => useUi.getState().toggleSidebar(),
  },
  {
    id: 'panel.toggle',
    title: 'Toggle Panel',
    section: 'View',
    keys: 'Ctrl+J',
    run: () => useUi.getState().togglePanel(),
  },
  {
    id: 'panel.logs',
    title: 'Show Logs',
    section: 'View',
    keys: 'Ctrl+Shift+U',
    run: () => useUi.getState().setPanelTab('logs'),
  },
  {
    id: 'panel.terminal',
    title: 'Show Terminal',
    section: 'View',
    run: () => useUi.getState().setPanelTab('terminal'),
  },
  {
    id: 'panel.preview',
    title: 'Show Preview',
    section: 'View',
    run: () => useUi.getState().setPanelTab('preview'),
  },
  {
    id: 'palette.open',
    title: 'Command Palette',
    section: 'View',
    keys: 'Ctrl+Shift+P',
    run: () => useUi.getState().setPalette(true),
  },
  {
    id: 'run.new',
    title: 'New Run',
    section: 'Run',
    keys: 'Ctrl+N',
    run: () => {
      useUi.getState().setView('runs')
      // The composer owns focus; announce intent via a DOM focus request.
      requestAnimationFrame(() => {
        document.querySelector<HTMLTextAreaElement>('[data-composer-input]')?.focus()
      })
    },
  },
  {
    id: 'run.startForge',
    title: 'Start Forge',
    section: 'Run',
    keys: 'F5',
    run: () => useUi.getState().setLauncherOpen(true),
    enabled: canStartForge,
    disabledReason: 'forge.exe is already running, or no binary is configured.',
  },
  {
    id: 'run.stopForge',
    title: 'Stop Forge',
    section: 'Run',
    keys: 'Shift+F5',
    run: () => stopForge(),
    enabled: canStopForge,
    disabledReason: 'No forge process is running.',
  },
  {
    id: 'connection.connect',
    title: 'Connect to Orchestrator',
    section: 'Run',
    run: () => connect(),
    enabled: () => useStudio.getState().connection.phase !== 'connected',
    disabledReason: 'Already connected.',
  },
  {
    id: 'connection.disconnect',
    title: 'Disconnect from Orchestrator',
    section: 'Run',
    run: () => disconnect(),
    enabled: () => useStudio.getState().connection.phase !== 'idle',
    disabledReason: 'Not connected.',
  },
  {
    id: 'graph.relayout',
    title: 'Re-layout Node Map',
    section: 'Graph',
    keys: 'Ctrl+Alt+L',
    run: () => graphHandlers['graph.relayout']?.(),
    enabled: () => Boolean(graphHandlers['graph.relayout']),
    disabledReason: 'The node map is not open.',
  },
  {
    id: 'graph.fit',
    title: 'Fit Node Map to View',
    section: 'Graph',
    keys: 'Ctrl+Alt+F',
    run: () => graphHandlers['graph.fit']?.(),
    enabled: () => Boolean(graphHandlers['graph.fit']),
    disabledReason: 'The node map is not open.',
  },
  {
    id: 'logs.clear',
    title: 'Clear Logs',
    section: 'Run',
    run: () => useStudio.getState().clearLogs(),
  },
  {
    id: 'panel.problems',
    title: 'Show Problems',
    section: 'View',
    run: () => useUi.getState().setPanelTab('problems'),
  },
  {
    id: 'workspace.reveal',
    title: 'Reveal Forge Folder',
    section: 'File',
    run: () => {
      const cwd = useStudio.getState().settings?.forge.cwd
      if (cwd) void bridge?.workspace.reveal(cwd)
    },
    enabled: () => Boolean(useStudio.getState().settings?.forge.cwd),
    disabledReason: 'No forge working directory is configured.',
  },
  {
    id: 'help.docs',
    title: 'Reticle Documentation',
    section: 'Help',
    run: () => void bridge?.workspace.openExternal('https://github.com/'),
  },
  {
    id: 'help.about',
    title: 'About Reticle Studio',
    section: 'Help',
    run: () => {
      useSettingsUi.getState().setSection('about')
      useUi.getState().setView('settings')
    },
  },

  // Window-level actions. These are commands rather than bare menu entries so
  // that the menu bar, the palette and the keybindings all resolve them the
  // same way — there is no native menu on Windows or Linux to provide them.
  {
    id: 'view.zoomIn',
    title: 'Zoom In',
    section: 'View',
    keys: 'Ctrl+=',
    run: () => void bridge?.native('zoomIn'),
  },
  {
    id: 'view.zoomOut',
    title: 'Zoom Out',
    section: 'View',
    keys: 'Ctrl+-',
    run: () => void bridge?.native('zoomOut'),
  },
  {
    id: 'view.zoomReset',
    title: 'Reset Zoom',
    section: 'View',
    keys: 'Ctrl+0',
    run: () => void bridge?.native('zoomReset'),
  },
  {
    id: 'view.fullScreen',
    title: 'Toggle Full Screen',
    section: 'View',
    keys: 'F11',
    run: () => void bridge?.native('toggleFullScreen'),
  },
  {
    id: 'view.devTools',
    title: 'Toggle Developer Tools',
    section: 'View',
    keys: 'Ctrl+Shift+I',
    run: () => void bridge?.native('toggleDevTools'),
  },
  {
    id: 'view.reload',
    title: 'Reload Window',
    section: 'View',
    keys: 'Ctrl+R',
    run: () => void bridge?.native('reload'),
  },
  {
    id: 'app.quit',
    title: 'Exit',
    section: 'File',
    run: () => void bridge?.native('quit'),
  },

  {
    id: 'theme.cycle',
    title: 'Cycle Theme',
    section: 'View',
    keys: 'Ctrl+Shift+L',
    run: () => setTheme(nextTheme(currentTheme())),
  },
  { id: 'theme.dark', title: 'Theme: Dark', section: 'View', run: () => setTheme('dark') },
  { id: 'theme.light', title: 'Theme: Light', section: 'View', run: () => setTheme('light') },
  {
    id: 'theme.system',
    title: 'Theme: Match System',
    section: 'View',
    run: () => setTheme('system'),
  },

  {
    id: 'graph.styleDetailed',
    title: 'Node Map: Detailed Nodes',
    section: 'Graph',
    run: () => setNodeStyle('detailed'),
  },
  {
    id: 'graph.styleCompact',
    title: 'Node Map: Compact Nodes',
    section: 'Graph',
    run: () => setNodeStyle('compact'),
  },
  {
    id: 'graph.styleHex',
    title: 'Node Map: Hexagonal Nodes',
    section: 'Graph',
    run: () => setNodeStyle('hex'),
  },
]

function currentTheme(): ThemePreference {
  return useStudio.getState().settings?.appearance.theme ?? 'dark'
}

function setTheme(theme: ThemePreference): void {
  void bridge?.settings.patch({ appearance: { theme } })
}

function setNodeStyle(nodeStyle: NodeStyle): void {
  void bridge?.settings.patch({ appearance: { nodeStyle } })
}

const BY_ID = new Map(COMMANDS.map((command) => [command.id, command]))

export function runCommand(id: CommandId): void {
  const command = BY_ID.get(id)
  if (!command) return
  if (command.enabled && !command.enabled()) return
  void command.run()
}

export function isEnabled(command: Command): boolean {
  return command.enabled ? command.enabled() : true
}

/** Match a keyboard event against a command's declared accelerator. */
export function matchKeybinding(event: KeyboardEvent): Command | undefined {
  const parts: string[] = []
  if (event.ctrlKey || event.metaKey) parts.push('Ctrl')
  if (event.shiftKey) parts.push('Shift')
  if (event.altKey) parts.push('Alt')

  const key = event.key.length === 1 ? event.key.toUpperCase() : event.key
  parts.push(key)
  const combo = parts.join('+')

  return COMMANDS.find((command) => command.keys === combo)
}
