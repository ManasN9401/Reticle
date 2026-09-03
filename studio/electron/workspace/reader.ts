import fs from 'node:fs/promises'
import path from 'node:path'
import { parse as parseYaml } from 'yaml'
import type { AgentCard, ApiResult, ReadFileResult, TreeEntry } from '../../src/shared/ipc'
import { builtinAgentsDir, envsDir, isInside, sessionsDir } from '../paths'

/** Files above this size are truncated rather than shipped whole to the renderer. */
const MAX_FILE_BYTES = 2 * 1024 * 1024

/** Directories that are large, uninteresting, or both. */
const SKIP_DIRS = new Set(['node_modules', '__pycache__', '.git', 'Lib', 'Scripts'])

function ok<T>(data: T): ApiResult<T> {
  return { ok: true, data }
}

function fail<T>(error: string): ApiResult<T> {
  return { ok: false, error }
}

function asStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  const out: string[] = []
  for (const item of value) {
    if (typeof item === 'string') {
      out.push(item)
    } else if (item && typeof item === 'object') {
      // Manifests drift: `inputs`/`outputs` appear both as bare strings and as
      // `{id, type, description}` objects. Accept either.
      const record = item as Record<string, unknown>
      const id = record.id ?? record.name ?? record.type
      if (typeof id === 'string') out.push(id)
    }
  }
  return out
}

function str(value: unknown): string | undefined {
  return typeof value === 'string' && value.length > 0 ? value : undefined
}

/**
 * Parse one agent manifest.
 *
 * `AgentDefinition` (runtime/agent/registry.go:21-36) does not declare
 * `capabilities`, `worker.command` or `required_skills`, but real manifests use
 * them — the Go loader drops them silently. Studio reads them anyway, because
 * the point of the agent card is to show what is actually written down.
 */
function toAgentCard(
  raw: unknown,
  manifestPath: string,
  origin: AgentCard['origin'],
  envIds: Set<string>,
): AgentCard | null {
  if (!raw || typeof raw !== 'object') return null
  const doc = raw as Record<string, unknown>
  const id = str(doc.id)
  if (!id) return null

  const worker = (doc.worker ?? {}) as Record<string, unknown>

  return {
    id,
    name: str(doc.name),
    description: str(doc.description),
    version: str(doc.version),
    runtime: str(doc.runtime) ?? str(worker.command),
    entrypoint:
      str(doc.entrypoint) ??
      (Array.isArray(worker.args) && typeof worker.args[0] === 'string'
        ? (worker.args[0] as string)
        : undefined),
    skills: [...asStringArray(doc.skills), ...asStringArray(doc.required_skills)],
    memory: asStringArray(doc.memory),
    capabilities: asStringArray(doc.capabilities),
    inputs: asStringArray(doc.inputs),
    outputs: asStringArray(doc.outputs),
    manifestPath,
    origin,
    hasEnv: envIds.has(id),
  }
}

async function readEnvIds(root: string): Promise<Set<string>> {
  try {
    const entries = await fs.readdir(envsDir(root), { withFileTypes: true })
    return new Set(entries.filter((e) => e.isDirectory()).map((e) => e.name))
  } catch {
    return new Set()
  }
}

async function loadManifest(
  file: string,
  origin: AgentCard['origin'],
  envIds: Set<string>,
): Promise<AgentCard | null> {
  try {
    const content = await fs.readFile(file, 'utf8')
    return toAgentCard(parseYaml(content), file, origin, envIds)
  } catch {
    // A malformed manifest should hide that one agent, not the whole roster.
    return null
  }
}

/** Both `.yml` and `.yaml` conventions coexist in this repo. */
function isManifest(name: string): boolean {
  return name.endsWith('.yaml') || name.endsWith('.yml')
}

async function collectAgentsFrom(
  dir: string,
  origin: AgentCard['origin'],
  envIds: Set<string>,
): Promise<AgentCard[]> {
  const cards: AgentCard[] = []
  let entries: import('node:fs').Dirent[]
  try {
    entries = await fs.readdir(dir, { withFileTypes: true })
  } catch {
    return cards
  }

  for (const entry of entries) {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      // Built-in agents are directories holding `definition.yml` or `<name>.yaml`.
      let inner: import('node:fs').Dirent[]
      try {
        inner = await fs.readdir(full, { withFileTypes: true })
      } catch {
        continue
      }
      for (const file of inner) {
        if (file.isFile() && isManifest(file.name)) {
          const card = await loadManifest(path.join(full, file.name), origin, envIds)
          if (card) cards.push(card)
        }
      }
    } else if (entry.isFile() && isManifest(entry.name)) {
      // Session agents are flat `<id>.yaml` files.
      const card = await loadManifest(full, origin, envIds)
      if (card) cards.push(card)
    }
  }
  return cards
}

export class WorkspaceReader {
  private root: string | null

  constructor(root: string | null) {
    this.root = root
  }

  setRoot(root: string | null): void {
    this.root = root
  }

  private requireRoot(): string | null {
    return this.root
  }

  async agents(execId?: string): Promise<ApiResult<AgentCard[]>> {
    const root = this.requireRoot()
    if (!root) return fail('Reticle repository root not found. Set it in Settings → Forge.')

    const envIds = await readEnvIds(root)
    const builtin = await collectAgentsFrom(builtinAgentsDir(root), 'builtin', envIds)

    let session: AgentCard[] = []
    if (execId) {
      session = await collectAgentsFrom(
        path.join(sessionsDir(root), execId, 'agents'),
        'session',
        envIds,
      )
    }

    // A session agent shadows a built-in with the same id — it is what actually ran.
    const byId = new Map<string, AgentCard>()
    for (const card of builtin) byId.set(card.id, card)
    for (const card of session) byId.set(card.id, card)

    return ok([...byId.values()].sort((a, b) => a.id.localeCompare(b.id)))
  }

  async tree(target?: string): Promise<ApiResult<TreeEntry[]>> {
    const root = this.requireRoot()
    if (!root) return fail('Reticle repository root not found.')

    const dir = target ? path.resolve(target) : sessionsDir(root)
    if (!isInside(root, dir)) {
      return fail('Refusing to read outside the Reticle repository.')
    }

    try {
      const entries = await fs.readdir(dir, { withFileTypes: true })
      const result: TreeEntry[] = []
      for (const entry of entries) {
        if (entry.isDirectory() && SKIP_DIRS.has(entry.name)) continue
        const full = path.join(dir, entry.name)
        let size: number | undefined
        if (entry.isFile()) {
          try {
            size = (await fs.stat(full)).size
          } catch {
            size = undefined
          }
        }
        result.push({
          name: entry.name,
          path: full,
          isDirectory: entry.isDirectory(),
          size,
        })
      }
      // Directories first, then alphabetical — the ordering every file tree uses.
      result.sort((a, b) => {
        if (a.isDirectory !== b.isDirectory) return a.isDirectory ? -1 : 1
        return a.name.localeCompare(b.name)
      })
      return ok(result)
    } catch (error) {
      return fail(error instanceof Error ? error.message : String(error))
    }
  }

  async read(target: string): Promise<ApiResult<ReadFileResult>> {
    const root = this.requireRoot()
    if (!root) return fail('Reticle repository root not found.')

    const file = path.resolve(target)
    if (!isInside(root, file)) {
      return fail('Refusing to read outside the Reticle repository.')
    }

    try {
      const stat = await fs.stat(file)
      if (!stat.isFile()) return fail('Not a file.')
      const truncated = stat.size > MAX_FILE_BYTES
      const handle = await fs.open(file, 'r')
      try {
        const length = truncated ? MAX_FILE_BYTES : stat.size
        const buffer = Buffer.alloc(length)
        await handle.read(buffer, 0, length, 0)
        return ok({
          path: file,
          content: buffer.toString('utf8'),
          truncated,
          size: stat.size,
        })
      } finally {
        await handle.close()
      }
    } catch (error) {
      return fail(error instanceof Error ? error.message : String(error))
    }
  }
}
