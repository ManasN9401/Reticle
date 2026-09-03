import { create } from 'zustand'

export type SettingsSection =
  | 'appearance'
  | 'connection'
  | 'forge'
  | 'models'
  | 'logs'
  | 'about'

export const SETTINGS_SECTIONS: { id: SettingsSection; label: string; hint: string }[] = [
  { id: 'appearance', label: 'Appearance', hint: 'Density and motion' },
  { id: 'connection', label: 'Connection', hint: 'Host, port, auto-connect' },
  { id: 'forge', label: 'Forge', hint: 'Binary, working directory, flags' },
  { id: 'models', label: 'Models', hint: 'Routing catalog and availability' },
  { id: 'logs', label: 'Logs', hint: 'Buffer size and tailing' },
  { id: 'about', label: 'About', hint: 'Versions and paths' },
]

interface SettingsUi {
  section: SettingsSection
  setSection(section: SettingsSection): void
}

export const useSettingsUi = create<SettingsUi>((set) => ({
  section: 'appearance',
  setSection: (section) => set({ section }),
}))
