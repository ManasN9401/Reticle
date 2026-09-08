import fs from 'node:fs'
import path from 'node:path'
import { app } from 'electron'
import type { SettingsPatch, StudioSettings } from '../src/shared/ipc'
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
    logs: {
      bufferSize: 50_000,
      followTail: true,
    },
  }
}

function settingsPath(): string {
  return path.join(app.getPath('userData'), FILE_NAME)
}

function mergeSection<T extends object>(base: T, patch: Partial<T> | undefined): T {
  return patch ? { ...base, ...patch } : base
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
