import { create } from 'zustand'

export type SettingsSection =
  | 'appearance'
  | 'connection'
  | 'forge'
  | 'keys'
  | 'models'
  | 'logs'
  | 'about'

export const SETTINGS_SECTIONS: {
  id: SettingsSection
  label: string
  hint: string
  /** Longer description shown in the section header. */
  description: string
}[] = [
  {
    id: 'appearance',
    label: 'Appearance',
    hint: 'Density and motion',
    description: 'How dense the interface is, and how much of it moves.',
  },
  {
    id: 'connection',
    label: 'Connection',
    hint: 'Host, port, auto-connect',
    description:
      'Where the orchestrator’s telemetry server is listening. The port must match the value forge was started with.',
  },
  {
    id: 'forge',
    label: 'Forge',
    hint: 'Binary, directory, flags',
    description:
      'How Studio launches forge.exe when you start it from here, and the flags it passes.',
  },
  {
    id: 'keys',
    label: 'API Keys',
    hint: 'Provider credentials',
    description:
      'Keys the router uses to reach model providers, stored in the repository’s .env file.',
  },
  {
    id: 'models',
    label: 'Models',
    hint: 'Routing catalog',
    description:
      'Every model the router discovered, which key each one is bound to, and whether it is currently usable.',
  },
  {
    id: 'logs',
    label: 'Logs',
    hint: 'Buffer and tailing',
    description: 'How much log history Studio keeps, and how the log panel behaves.',
  },
  {
    id: 'about',
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
