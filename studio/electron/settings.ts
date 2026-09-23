import fs from 'node:fs'
import path from 'node:path'
import { app } from 'electron'
import type { ColorScheme, LayoutSnapshot, NamedLayout, SettingsPatch, SettingsProfile, StudioSettings } from '../src/shared/ipc'
import { COLOR_TOKENS, isSafeColorValue } from '../src/shared/ipc'
import { defaultForgeBinary, findRepoRoot, forgeDir } from './paths'
import { requireLocalHost } from './forge/auth'

const FILE_NAME = 'settings.json'

function defaults(): StudioSettings {
  const root = findRepoRoot()
  return {
    connection: {
      host: '127.0.0.1',
      port: 8080,
      autoConnect: true,
    },
    forge: {
      binaryPath: root ? defaultForgeBinary(root) : '',
      cwd: root ? forgeDir(root) : '',
      batch: 5,
      retries: 3,
      isolated: true,
      allModels: false,
      native: false,
    },
    appearance: {
      theme: 'dark',
      density: 'comfortable',
      reduceMotion: false,
      nodeStyle: 'detailed',
    },
    nodeAppearance: {
      hexShape: 'hexagon',
      detailed: {
        scale: 1,
        fields: { modelChip: true, nodeId: true, artifactCount: true, retryCount: true, duration: true },
      },
      compact: {
        scale: 1,
        fields: { duration: true, icons: true },
      },
      hex: {
        scale: 1,
        fields: { label: true, durationOnHover: true },
      },
    },
    colorSchemes: {
      schemes: [],
      activeId: null,
    },
    profiles: {
      profiles: [],
      activeId: null,
    },
    tabLayouts: {
      autoPersist: defaultLayoutSnapshot(),
      saved: [],
    },
    logs: {
      bufferSize: 50_000,
      followTail: true,
    },
  }
}

function defaultLayoutSnapshot(): LayoutSnapshot {
  return {
    activeView: 'runs',
    sidebarOpen: true,
    sidebarWidth: 288,
    panelOpen: true,
    panelTab: 'activity',
    panelHeight: 260,
    panelMaximized: false,
    tabs: [{ id: 'graph', kind: 'graph', title: 'Node Map' }],
    activeTabId: 'graph',
    inspectorOpen: true,
  }
}

function settingsPath(): string {
  return path.join(app.getPath('userData'), FILE_NAME)
}

function mergeSection<T extends object>(base: T, patch: Partial<T> | undefined): T {
  return patch ? { ...base, ...patch } : base
}

const HEX_SHAPES = new Set(['hexagon', 'octagon', 'circle'])
const MIN_NODE_SCALE = 0.85
const MAX_NODE_SCALE = 1.35

function validateStyleAppearance(
  value: { scale: number; fields: unknown } | undefined,
  name: string,
  fieldKeys: readonly string[],
): void {
  if (!value || typeof value.scale !== 'number' || value.scale < MIN_NODE_SCALE || value.scale > MAX_NODE_SCALE) {
    throw new Error(`Invalid ${name} scale`)
  }
  if (!value.fields || typeof value.fields !== 'object') throw new Error(`Invalid ${name} fields`)
  const fields = value.fields as Record<string, unknown>
  for (const key of fieldKeys) {
    if (typeof fields[key] !== 'boolean') throw new Error(`Invalid ${name} field ${key}`)
  }
}

export function validateNodeAppearance(value: StudioSettings['nodeAppearance']): void {
  if (!value || !HEX_SHAPES.has(value.hexShape)) throw new Error('Invalid node appearance')
  validateStyleAppearance(value.detailed, 'detailed', ['modelChip', 'nodeId', 'artifactCount', 'retryCount', 'duration'])
  validateStyleAppearance(value.compact, 'compact', ['duration', 'icons'])
  validateStyleAppearance(value.hex, 'hex', ['label', 'durationOnHover'])
}

export function validateColorSchemes(value: StudioSettings['colorSchemes']): void {
  if (!value || !Array.isArray(value.schemes)) throw new Error('Invalid colour schemes')
  const ids = new Set<string>()
  for (const scheme of value.schemes as ColorScheme[]) {
    if (typeof scheme.id !== 'string' || !scheme.id) throw new Error('Invalid colour scheme id')
    if (ids.has(scheme.id)) throw new Error('Duplicate colour scheme id')
    ids.add(scheme.id)
    if (typeof scheme.name !== 'string' || !scheme.name.trim()) throw new Error('Invalid colour scheme name')
    if (scheme.base !== 'dark' && scheme.base !== 'light') throw new Error('Invalid colour scheme base')
    if (typeof scheme.tokens !== 'object' || scheme.tokens === null) throw new Error('Invalid colour scheme tokens')
    for (const [token, val] of Object.entries(scheme.tokens)) {
      if (!(COLOR_TOKENS as readonly string[]).includes(token)) throw new Error(`Unknown colour token ${token}`)
      if (typeof val !== 'string' || !isSafeColorValue(val)) throw new Error(`Invalid colour value for ${token}`)
    }
  }
  if (value.activeId !== null && !ids.has(value.activeId)) throw new Error('activeId does not match any scheme')
}

const VIEW_IDS = new Set(['runs', 'graph', 'agents', 'artifacts', 'explorer', 'settings'])
const PANEL_TABS = new Set(['activity', 'logs', 'problems', 'terminal', 'preview'])

function validateLayoutSnapshot(value: LayoutSnapshot | undefined, name: string): void {
  if (!value) throw new Error(`Invalid ${name}`)
  if (!VIEW_IDS.has(value.activeView)) throw new Error(`Invalid ${name} activeView`)
  if (!PANEL_TABS.has(value.panelTab)) throw new Error(`Invalid ${name} panelTab`)
  if (!Array.isArray(value.tabs)) throw new Error(`Invalid ${name} tabs`)
  for (const tab of value.tabs) {
    if (typeof tab.id !== 'string' || !tab.id) throw new Error(`Invalid ${name} tab id`)
    if (tab.kind !== 'graph' && tab.kind !== 'file') throw new Error(`Invalid ${name} tab kind`)
  }
  if (typeof value.activeTabId !== 'string' || !value.activeTabId) throw new Error(`Invalid ${name} activeTabId`)
  for (const flag of [value.sidebarOpen, value.panelOpen, value.panelMaximized, value.inspectorOpen]) {
    if (typeof flag !== 'boolean') throw new Error(`Invalid ${name} flags`)
  }
  if (!Number.isFinite(value.sidebarWidth) || !Number.isFinite(value.panelHeight)) {
    throw new Error(`Invalid ${name} dimensions`)
  }
}

function validateTabLayouts(value: StudioSettings['tabLayouts']): void {
  if (!value || !Array.isArray(value.saved)) throw new Error('Invalid tab layouts')
  validateLayoutSnapshot(value.autoPersist, 'auto-persisted layout')
  for (const layout of value.saved as NamedLayout[]) {
    if (typeof layout.id !== 'string' || !layout.id) throw new Error('Invalid saved layout id')
    if (typeof layout.name !== 'string' || !layout.name.trim()) throw new Error('Invalid saved layout name')
    validateLayoutSnapshot(layout.snapshot, `saved layout "${layout.name}"`)
  }
}

function validateProfiles(value: StudioSettings['profiles']): void {
  if (!value || !Array.isArray(value.profiles)) throw new Error('Invalid profiles')
  const ids = new Set<string>()
  for (const profile of value.profiles as SettingsProfile[]) {
    if (typeof profile.id !== 'string' || !profile.id) throw new Error('Invalid profile id')
    if (ids.has(profile.id)) throw new Error('Duplicate profile id')
    ids.add(profile.id)
    if (typeof profile.name !== 'string' || !profile.name.trim()) throw new Error('Invalid profile name')
    const snapshot = profile.snapshot
    if (!snapshot || typeof snapshot !== 'object') throw new Error('Invalid profile snapshot')
    validateNodeAppearance(snapshot.nodeAppearance)
    validateColorSchemes(snapshot.colorSchemes)
    if (!snapshot.appearance || typeof snapshot.appearance !== 'object') throw new Error('Invalid profile appearance')
    if (!snapshot.connection || typeof snapshot.connection !== 'object') throw new Error('Invalid profile connection')
  }
  if (value.activeId !== null && !ids.has(value.activeId)) throw new Error('activeId does not match any profile')
}

function validate(value: StudioSettings): StudioSettings {
  requireLocalHost(value.connection.host)
  for (const [name, number, min, max] of [
    ['port', value.connection.port, 1, 65535],
    ['batch', value.forge.batch, 1, 16],
    ['attempts', value.forge.retries, 1, 15],
    ['log buffer', value.logs.bufferSize, 100, 100000],
  ] as const) {
    if (!Number.isInteger(number) || number < min || number > max) throw new Error(`Invalid ${name}`)
  }
  for (const flag of [value.connection.autoConnect, value.forge.isolated, value.forge.allModels, value.forge.native, value.logs.followTail]) {
    if (typeof flag !== 'boolean') throw new Error('Settings flags must be booleans')
  }
  if (typeof value.forge.cwd !== 'string' || typeof value.forge.binaryPath !== 'string') throw new Error('Forge paths must be strings')
  validateNodeAppearance(value.nodeAppearance)
  validateColorSchemes(value.colorSchemes)
  validateProfiles(value.profiles)
  validateTabLayouts(value.tabLayouts)
  return value
}

export class SettingsStore {
  private current: StudioSettings

  constructor() {
    this.current = this.load()
  }

  private load(): StudioSettings {
    const base = defaults()
    try {
      const raw = fs.readFileSync(settingsPath(), 'utf8')
      const stored = JSON.parse(raw) as SettingsPatch
      return validate({
        connection: mergeSection(base.connection, stored.connection),
        forge: mergeSection(base.forge, stored.forge),
        appearance: mergeSection(base.appearance, stored.appearance),
        nodeAppearance: mergeSection(base.nodeAppearance, stored.nodeAppearance),
        colorSchemes: mergeSection(base.colorSchemes, stored.colorSchemes),
        profiles: mergeSection(base.profiles, stored.profiles),
        tabLayouts: mergeSection(base.tabLayouts, stored.tabLayouts),
        logs: mergeSection(base.logs, stored.logs),
      })
    } catch {
      // Missing or corrupt settings must never block startup — fall back to
      // defaults and let the next write repair the file.
      return base
    }
  }

  get(): StudioSettings {
    return this.current
  }

  patch(patch: SettingsPatch): StudioSettings {
    this.current = validate({
      connection: mergeSection(this.current.connection, patch.connection),
      forge: mergeSection(this.current.forge, patch.forge),
      appearance: mergeSection(this.current.appearance, patch.appearance),
      nodeAppearance: mergeSection(this.current.nodeAppearance, patch.nodeAppearance),
      colorSchemes: mergeSection(this.current.colorSchemes, patch.colorSchemes),
      profiles: mergeSection(this.current.profiles, patch.profiles),
      tabLayouts: mergeSection(this.current.tabLayouts, patch.tabLayouts),
      logs: mergeSection(this.current.logs, patch.logs),
    })
    this.persist()
    return this.current
  }

  private persist(): void {
    try {
      const target = settingsPath()
      fs.mkdirSync(path.dirname(target), { recursive: true })
      fs.writeFileSync(target + '.tmp', JSON.stringify(this.current, null, 2), {encoding:'utf8', mode:0o600})
      fs.renameSync(target + '.tmp', target)
    } catch (error) {
      console.error('[settings] failed to persist', error)
    }
  }
}
