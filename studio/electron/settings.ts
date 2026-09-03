import fs from 'node:fs'
import path from 'node:path'
import { app } from 'electron'
import type { SettingsPatch, StudioSettings } from '../src/shared/ipc'
import { defaultForgeBinary, findRepoRoot, forgeDir } from './paths'

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
      retries: 15,
      isolated: true,
      allModels: false,
    },
    appearance: {
      density: 'comfortable',
      reduceMotion: false,
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
      return {
        connection: mergeSection(base.connection, stored.connection),
        forge: mergeSection(base.forge, stored.forge),
        appearance: mergeSection(base.appearance, stored.appearance),
        logs: mergeSection(base.logs, stored.logs),
      }
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
    this.current = {
      connection: mergeSection(this.current.connection, patch.connection),
      forge: mergeSection(this.current.forge, patch.forge),
      appearance: mergeSection(this.current.appearance, patch.appearance),
      logs: mergeSection(this.current.logs, patch.logs),
    }
    this.persist()
    return this.current
  }

  private persist(): void {
    try {
      const target = settingsPath()
      fs.mkdirSync(path.dirname(target), { recursive: true })
      fs.writeFileSync(target, JSON.stringify(this.current, null, 2), 'utf8')
    } catch (error) {
      console.error('[settings] failed to persist', error)
    }
  }
}
