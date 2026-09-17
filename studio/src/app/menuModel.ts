import type { CommandId, NativeAction } from '@shared/ipc'

/**
 * The menu bar's contents.
 *
 * Items are declarative so the bar itself stays a pure renderer. Anything that
 * does real work resolves through the shared command registry; the only
 * exceptions are the Edit entries, which map to main-process actions on the
 * focused webContents because there is no native menu to supply the usual
 * `role` handlers.
 */

export type MenuItem =
  | { kind: 'command'; id: CommandId; label?: string }
  | { kind: 'native'; action: NativeAction; label: string; keys?: string }
  | { kind: 'separator' }
  /** Rendered greyed out with the reason as a tooltip — never a silent no-op. */
  | { kind: 'unavailable'; label: string; reason: string }

export interface MenuDefinition {
  /** Displayed label. The character after `&` is the Alt mnemonic. */
  label: string
  items: MenuItem[]
}

export const MENUS: MenuDefinition[] = [
  {
    label: '&File',
    items: [
      { kind: 'command', id: 'run.new', label: 'New Run…' },
      { kind: 'separator' },
      { kind: 'command', id: 'view.settings', label: 'Settings' },
      { kind: 'command', id: 'workspace.reveal' },
      { kind: 'separator' },
      { kind: 'command', id: 'app.quit' },
    ],
  },
  {
    label: '&Edit',
    items: [
      { kind: 'native', action: 'undo', label: 'Undo', keys: 'Ctrl+Z' },
      { kind: 'native', action: 'redo', label: 'Redo', keys: 'Ctrl+Y' },
      { kind: 'separator' },
      { kind: 'native', action: 'cut', label: 'Cut', keys: 'Ctrl+X' },
      { kind: 'native', action: 'copy', label: 'Copy', keys: 'Ctrl+C' },
      { kind: 'native', action: 'paste', label: 'Paste', keys: 'Ctrl+V' },
      { kind: 'native', action: 'selectAll', label: 'Select All', keys: 'Ctrl+A' },
    ],
  },
  {
    label: '&View',
    items: [
      { kind: 'command', id: 'palette.open', label: 'Command Palette…' },
      { kind: 'separator' },
      { kind: 'command', id: 'view.runs', label: 'Runs' },
      { kind: 'command', id: 'view.graph', label: 'Node Map' },
      { kind: 'command', id: 'view.agents', label: 'Agents' },
      { kind: 'command', id: 'view.artifacts', label: 'Artifacts' },
      { kind: 'command', id: 'view.explorer', label: 'Explorer' },
      { kind: 'separator' },
      { kind: 'command', id: 'sidebar.toggle' },
      { kind: 'command', id: 'panel.toggle' },
      { kind: 'separator' },
      { kind: 'command', id: 'theme.cycle' },
      { kind: 'command', id: 'theme.dark' },
      { kind: 'command', id: 'theme.light' },
      { kind: 'command', id: 'theme.system' },
      { kind: 'separator' },
      { kind: 'command', id: 'view.zoomIn' },
      { kind: 'command', id: 'view.zoomOut' },
      { kind: 'command', id: 'view.zoomReset' },
      { kind: 'separator' },
      { kind: 'command', id: 'view.fullScreen' },
      { kind: 'command', id: 'view.reload' },
      { kind: 'command', id: 'view.devTools' },
    ],
  },
  {
    label: '&Run',
    items: [
      { kind: 'command', id: 'run.startForge' },
      { kind: 'command', id: 'run.stopForge' },
      { kind: 'separator' },
      { kind: 'command', id: 'connection.connect', label: 'Connect' },
      { kind: 'command', id: 'connection.disconnect', label: 'Disconnect' },
      { kind: 'separator' },
      { kind: 'command', id: 'graph.relayout', label: 'Re-layout Node Map' },
      { kind: 'command', id: 'graph.fit', label: 'Fit Node Map' },
      { kind: 'separator' },
      { kind: 'command', id: 'graph.styleDetailed', label: 'Detailed Nodes' },
      { kind: 'command', id: 'graph.styleCompact', label: 'Compact Nodes' },
      { kind: 'command', id: 'graph.styleHex', label: 'Hexagonal Nodes' },
    ],
  },
  {
    label: '&Terminal',
    items: [
      { kind: 'command', id: 'panel.terminal', label: 'Show Terminal' },
      { kind: 'command', id: 'panel.activity', label: 'Show Activity' },
      { kind: 'command', id: 'panel.logs', label: 'Show Logs' },
      { kind: 'command', id: 'panel.problems', label: 'Show Problems' },
      { kind: 'command', id: 'panel.preview', label: 'Show Preview' },
      { kind: 'separator' },
      { kind: 'command', id: 'logs.clear' },
      { kind: 'separator' },
      {
        kind: 'unavailable',
        label: 'New Terminal',
        reason:
          'Not wired up yet — the Terminal panel is a placeholder until a pty backend exists.',
      },
    ],
  },
  {
    label: '&Help',
    items: [
      { kind: 'command', id: 'help.docs' },
      { kind: 'command', id: 'help.about' },
    ],
  },
]

/** Split a `&`-marked label into its display text and Alt mnemonic. */
export function parseMnemonic(label: string): { text: string; key?: string } {
  const index = label.indexOf('&')
  if (index === -1 || index === label.length - 1) return { text: label }
  return {
    text: label.slice(0, index) + label.slice(index + 1),
    key: label[index + 1].toLowerCase(),
  }
}
