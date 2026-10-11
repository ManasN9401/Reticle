import fs from 'node:fs'
import path from 'node:path'

export interface StaleBinary {
  /** The Go source file that was changed after the binary was built. */
  source: string
  builtAt: number
  changedAt: number
}

// compiler/ holds the Python workers, which are read from source on every run and cannot be stale.
const SKIPPED = new Set(['node_modules', '.git', '.reticle', 'compiler', 'vendor', 'testdata'])
const MAX_DEPTH = 6
/** Editors and checkouts touch files within moments of a build; only a clear gap counts. */
const TOLERANCE_MS = 2000

function newestSource(dir: string, depth: number): { file: string; at: number } | null {
  if (depth > MAX_DEPTH) return null
  let entries: fs.Dirent[]
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true })
  } catch {
    return null
  }
  let newest: { file: string; at: number } | null = null
  for (const entry of entries) {
    const full = path.join(dir, entry.name)
    let candidate: { file: string; at: number } | null = null
    if (entry.isDirectory()) {
      if (!SKIPPED.has(entry.name)) candidate = newestSource(full, depth + 1)
    } else if (entry.name.endsWith('.go') && !entry.name.endsWith('_test.go')) {
      try {
        candidate = { file: full, at: fs.statSync(full).mtimeMs }
      } catch {
        candidate = null
      }
    }
    if (candidate && (!newest || candidate.at > newest.at)) newest = candidate
  }
  return newest
}

/**
 * The runtime fixes that matter live in the Go binary, which Studio launches as built and never
 * rebuilds. Returns the newest Go source changed after the binary, or null when the binary is
 * current or cannot be judged (a packaged app has no sources to compare with).
 */
export function staleBinary(binaryPath: string, repoRoot: string): StaleBinary | null {
  let builtAt: number
  try {
    builtAt = fs.statSync(binaryPath).mtimeMs
  } catch {
    return null
  }
  let newest: { file: string; at: number } | null = null
  for (const dir of [path.join(repoRoot, 'runtime'), path.join(repoRoot, 'cmd', 'forge')]) {
    const found = newestSource(dir, 0)
    if (found && (!newest || found.at > newest.at)) newest = found
  }
  if (!newest || newest.at <= builtAt + TOLERANCE_MS) return null
  return { source: path.relative(repoRoot, newest.file), builtAt, changedAt: newest.at }
}

export function describeStaleBinary(stale: StaleBinary): string {
  const when = (ms: number) => new Date(ms).toLocaleString()
  return (
    `forge was built ${when(stale.builtAt)} but ${stale.source} changed ${when(stale.changedAt)}, ` +
    'so runtime fixes made since then are not running. Rebuild with: cd cmd/forge && go build -o forge.exe'
  )
}
