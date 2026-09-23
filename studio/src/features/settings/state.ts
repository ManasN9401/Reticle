import { create } from 'zustand'

/**
 * Groups exist only once a section needs one — an empty group header helps no
 * one. `context` (Shared Context) and `extensions` (MCP Servers, Plugins) join
 * this list when those sections land.
 */
export type SettingsGroup = 'general' | 'profile' | 'connection' | 'credentials' | 'diagnostics'

export const SETTINGS_GROUPS: { id: SettingsGroup; label: string }[] = [
  { id: 'general', label: 'General' },
  { id: 'profile', label: 'Profile' },
  { id: 'connection', label: 'Connection' },
  { id: 'credentials', label: 'Credentials & Models' },
  { id: 'diagnostics', label: 'Diagnostics' },
]

export type SettingsSection =
  | 'appearance'
  | 'nodeAppearance'
  | 'profile'
  | 'tabLayouts'
  | 'connection'
  | 'forge'
  | 'keys'
  | 'models'
  | 'logs'
  | 'about'

export const SETTINGS_SECTIONS: {
  id: SettingsSection
  group: SettingsGroup
  label: string
  hint: string
  /** Longer description shown in the section header. */
  description: string
}[] = [
  {
    id: 'appearance',
    group: 'general',
    label: 'Appearance',
    hint: 'Theme, colour schemes, density',
    description:
      'Density and motion, dark/light/custom theme, and the colour schemes layered on top of it.',
  },
  {
    id: 'nodeAppearance',
    group: 'general',
    label: 'Node Appearance',
    hint: 'Fields, size, hex shape',
    description:
      'Which data each node map style draws, how large it draws it, and the hexagon-style shape. Status colour lives in Appearance › Colour Schemes.',
  },
  {
    id: 'profile',
    group: 'profile',
    label: 'Profile',
    hint: 'Save & switch config bundles',
    description:
      'Save the current Appearance, Node Appearance, Colour Scheme and Connection settings as a named, switchable profile.',
  },
  {
    id: 'tabLayouts',
    group: 'profile',
    label: 'Tab Layouts',
    hint: 'Panel & tab arrangements',
    description:
      'Your current arrangement of sidebar, panels and tabs is remembered automatically. Save named arrangements here to switch between them.',
  },
  {
    id: 'connection',
    group: 'connection',
    label: 'Connection',
    hint: 'Host, port, auto-connect',
    description:
      'Where the orchestrator’s telemetry server is listening. The port must match the value forge was started with.',
  },
  {
    id: 'forge',
    group: 'connection',
    label: 'Forge',
    hint: 'Binary, directory, flags',
    description:
      'How Studio launches forge.exe when you start it from here, and the flags it passes.',
  },
  {
    id: 'keys',
    group: 'credentials',
    label: 'API Keys',
    hint: 'Provider credentials',
    description:
      'Keys the router uses to reach model providers, stored in the repository’s .env file.',
  },
  {
    id: 'models',
    group: 'credentials',
    label: 'Models',
    hint: 'Routing catalog',
    description:
      'Every model the router discovered, which key each one is bound to, and whether it is currently usable.',
  },
  {
    id: 'logs',
    group: 'diagnostics',
    label: 'Logs',
    hint: 'Buffer and tailing',
    description: 'How much log history Studio keeps, and how the log panel behaves.',
  },
  {
    id: 'about',
    group: 'diagnostics',
    label: 'About',
    hint: 'Versions and paths',
    description: 'Build information and the paths this session resolved.',
  },
]

interface SettingsUi {
  section: SettingsSection
  setSection(section: SettingsSection): void
}

export const useSettingsUi = create<SettingsUi>((set) => ({
  section: 'appearance',
  setSection: (section) => set({ section }),
}))
