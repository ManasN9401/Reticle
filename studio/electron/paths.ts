import fs from 'node:fs'
import path from 'node:path'
import { app } from 'electron'

/**
 * Locating the Reticle repository root.
 *
 * This matters more than it looks. `cmd/forge/main.go:166` resolves its root as
 * `filepath.Abs("../../")`, and the logger and waitlist reach for `../../logs`
 * and `../../.reticle`. So forge.exe must be launched with its cwd set exactly
 * to `<repo>/cmd/forge` — anywhere else and sessions, logs and artifacts all
 * resolve to the wrong place, which surfaces in the UI as silently empty
 * panels rather than an error.
 */

/** A directory is the repo root if it has the two things forge needs. */
function looksLikeRepoRoot(dir: string): boolean {
  return (
    fs.existsSync(path.join(dir, 'cmd', 'forge')) &&
    fs.existsSync(path.join(dir, 'runtime'))
  )
}

function walkUp(start: string, limit = 6): string | null {
  let current = path.resolve(start)
  for (let i = 0; i < limit; i += 1) {
    if (looksLikeRepoRoot(current)) return current
    const parent = path.dirname(current)
    if (parent === current) break
    current = parent
  }
  return null
}

let cachedRoot: string | null | undefined

/**
 * Resolve the repo root, or null when Studio is running detached from a
 * checkout (a packaged install elsewhere on disk). Callers must handle null by
 * asking the user for the path rather than guessing.
 */
export function findRepoRoot(): string | null {
  if (cachedRoot !== undefined) return cachedRoot

  // `app` is only available inside Electron. Reading it lazily keeps this
  // module usable from plain Node (tests, tooling) and means an explicit
  // RETICLE_ROOT never depends on the Electron runtime being present.
  let appPaths: string[] = []
  try {
    appPaths = [
      app.isPackaged ? path.dirname(app.getPath('exe')) : '',
      app.getAppPath(),
    ]
  } catch {
    appPaths = []
  }

  const candidates = [
    process.env.RETICLE_ROOT,
    // Dev: cwd is studio/
    process.cwd(),
    // Packaged: resources/app.asar -> walk out
    ...appPaths,
  ].filter((c): c is string => typeof c === 'string' && c.length > 0)

  for (const candidate of candidates) {
    if (looksLikeRepoRoot(candidate)) {
      cachedRoot = candidate
      return cachedRoot
    }
    const found = walkUp(candidate)
    if (found) {
      cachedRoot = found
      return cachedRoot
    }
  }

  cachedRoot = null
  return null
}

/** Allow the user to override an incorrect or undiscoverable root. */
export function setRepoRoot(root: string | null): void {
  cachedRoot = root ?? undefined
}

export function forgeDir(root: string): string {
  return path.join(root, 'cmd', 'forge')
}

/** Windows ships forge.exe; other platforms build a bare `forge`. */
export function defaultForgeBinary(root: string): string {
  const dir = forgeDir(root)
  const exe = path.join(dir, process.platform === 'win32' ? 'forge.exe' : 'forge')
  return exe
}

export function sessionsDir(root: string): string {
  return path.join(root, '.reticle', 'sessions')
}

export function envsDir(root: string): string {
  return path.join(root, '.reticle', 'envs')
}

export function runtimeLogPath(root: string): string {
  return path.join(root, 'logs', 'runtime.log')
}

export function builtinAgentsDir(root: string): string {
  return path.join(root, 'cmd', 'forge', 'compiler', 'agents')
}

/**
 * Guard every filesystem read the renderer can trigger. Without this, a crafted
 * path from the renderer could read anything the user can read.
 */
export function isInside(parent: string, child: string): boolean {
  const rel = path.relative(path.resolve(parent), path.resolve(child))
  return rel === '' || (!rel.startsWith('..') && !path.isAbsolute(rel))
}
