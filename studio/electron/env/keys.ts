import fs from 'node:fs/promises'
import path from 'node:path'
import type { ApiResult, EnvKeyEntry, KeyProvider } from '../../src/shared/ipc'
import { findRepoRoot } from '../paths'

/**
 * API key management, backed by the repo-root `.env`.
 *
 * `cmd/forge/main.go:23-38` parses that file itself — a plain `KEY=VALUE` scan
 * with `#` comments, no quote handling, value is everything after the first `=`
 * trimmed. So we write it back in exactly that shape, and never add quoting the
 * runtime would then treat as part of the secret.
 *
 * Two consequences worth surfacing in the UI rather than hiding:
 *  - `loadEnv` runs once at boot, so edits only take effect on a forge restart.
 *  - the router scans a fixed set of variable names (routing/models.go:100,
 *    :194, :255). A key stored under any other name is simply never read.
 */

/** The only variable names the router looks for. */
const KNOWN_SLOTS: { name: string; provider: KeyProvider; label: string }[] = [
  { name: 'OPENROUTER_API_KEY', provider: 'openrouter', label: 'Primary' },
  { name: 'OPENROUTER_API_KEY_2', provider: 'openrouter', label: 'Secondary' },
  { name: 'OPENROUTER_API_KEY_3', provider: 'openrouter', label: 'Tertiary' },
  { name: 'GROQ_API_KEY', provider: 'groq', label: 'Primary' },
  { name: 'GROQ_API_KEY_2', provider: 'groq', label: 'Secondary' },
  { name: 'GROQ_API_KEY_3', provider: 'groq', label: 'Tertiary' },
  { name: 'GEMINI_API_KEY', provider: 'gemini', label: 'Primary' },
]

const VAR_LINE = /^(\s*)([A-Za-z_][A-Za-z0-9_]*)(\s*=\s*)(.*)$/

function envPath(): string | null {
  const root = findRepoRoot()
  return root ? path.join(root, '.env') : null
}

async function readLines(): Promise<string[]> {
  const target = envPath()
  if (!target) return []
  try {
    const raw = await fs.readFile(target, 'utf8')
    return raw.split(/\r?\n/)
  } catch {
    // A missing .env is a normal first-run state, not an error.
    return []
  }
}

/**
 * Show enough to identify a key without exposing it. Full values only ever
 * leave the main process through an explicit reveal.
 */
function mask(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return ''
  if (trimmed.length <= 8) return '•'.repeat(trimmed.length)
  return `${'•'.repeat(8)}${trimmed.slice(-4)}`
}

function parse(lines: string[]): Map<string, string> {
  const found = new Map<string, string>()
  for (const line of lines) {
    if (/^\s*#/.test(line)) continue
    const match = VAR_LINE.exec(line)
    if (!match) continue
    found.set(match[2], match[4].trim())
  }
  return found
}

export async function listKeys(): Promise<ApiResult<EnvKeyEntry[]>> {
  const target = envPath()
  if (!target) {
    return { ok: false, error: 'Reticle repository root not found, so .env cannot be located.' }
  }

  const values = parse(await readLines())
  const entries: EnvKeyEntry[] = KNOWN_SLOTS.map((slot) => {
    const value = values.get(slot.name) ?? ''
    return {
      name: slot.name,
      provider: slot.provider,
      slotLabel: slot.label,
      known: true,
      present: value.length > 0,
      masked: mask(value),
      length: value.length,
    }
  })

  // Anything else in the file is shown too, but flagged: the router will not
  // read it, and silently ignoring it would be worse than saying so.
  for (const [name, value] of values) {
    if (KNOWN_SLOTS.some((slot) => slot.name === name)) continue
    entries.push({
      name,
      provider: 'other',
      known: false,
      present: value.length > 0,
      masked: mask(value),
      length: value.length,
    })
  }

  return { ok: true, data: entries }
}

export async function revealKey(name: string): Promise<ApiResult<string>> {
  if (!VAR_LINE.test(`${name}=`)) return { ok: false, error: 'Invalid variable name.' }
  const values = parse(await readLines())
  return { ok: true, data: values.get(name) ?? '' }
}

/**
 * Set or replace one variable, preserving every other line, its ordering and
 * its comments — this file is hand-edited and may hold things we know nothing
 * about.
 */
export async function setKey(name: string, value: string): Promise<ApiResult<void>> {
  const target = envPath()
  if (!target) return { ok: false, error: 'Reticle repository root not found.' }
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) {
    return { ok: false, error: 'Variable names may contain only letters, digits and underscores.' }
  }

  const clean = value.trim()
  if (clean.includes('\n')) return { ok: false, error: 'A key cannot contain a newline.' }

  const lines = await readLines()
  let replaced = false
  const next = lines.map((line) => {
    if (/^\s*#/.test(line)) return line
    const match = VAR_LINE.exec(line)
    if (!match || match[2] !== name) return line
    replaced = true
    return `${match[1]}${match[2]}${match[3]}${clean}`
  })

  if (!replaced) {
    if (next.length > 0 && next[next.length - 1].trim() === '') next.pop()
    next.push(`${name}=${clean}`)
  }

  return write(target, next)
}

export async function removeKey(name: string): Promise<ApiResult<void>> {
  const target = envPath()
  if (!target) return { ok: false, error: 'Reticle repository root not found.' }

  const lines = await readLines()
  const next = lines.filter((line) => {
    if (/^\s*#/.test(line)) return true
    const match = VAR_LINE.exec(line)
    return !match || match[2] !== name
  })

  if (next.length === lines.length) return { ok: false, error: `${name} is not set.` }
  return write(target, next)
}

/** Write via a temp file and rename, so a crash cannot truncate .env. */
async function write(target: string, lines: string[]): Promise<ApiResult<void>> {
  const body = lines.join('\n').replace(/\n+$/, '') + '\n'
  const temp = `${target}.studio-tmp`
  try {
    await fs.writeFile(temp, body, { encoding: 'utf8', mode: 0o600 })
    await fs.rename(temp, target)
    return { ok: true }
  } catch (error) {
    await fs.rm(temp, { force: true }).catch(() => {})
    return { ok: false, error: error instanceof Error ? error.message : String(error) }
  }
}

export function envFilePath(): string | null {
  return envPath()
}
