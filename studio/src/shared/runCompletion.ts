import type { ProjectionState, Run, RunNode } from './projection'

export interface RunCompletion {
  execId: string
  outcome: 'completed' | 'failed'
  run: Run
}

/** Architect planning runs are named compile-<exec>; they are not the user's product. */
export function isCompileRun(execId: string): boolean {
  return execId.startsWith('compile-')
}

/**
 * Runs that reached a terminal status between two projections. A run that was
 * unknown or already terminal before the update is not news: that is what a
 * reconnect or replay looks like, and it must not toast old runs.
 */
export function detectCompletions(
  previous: ProjectionState | null | undefined,
  next: ProjectionState,
): RunCompletion[] {
  if (!previous) return []
  const found: RunCompletion[] = []
  for (const execId of next.runOrder) {
    const run = next.runs[execId]
    if (!run || isCompileRun(execId)) continue
    if (run.status !== 'completed' && run.status !== 'failed') continue
    const before = previous.runs[execId]
    if (!before || before.status === 'completed' || before.status === 'failed') continue
    found.push({ execId, outcome: run.status, run })
  }
  return found
}

/**
 * Nodes that have just started waiting for a person. Like completions, only a live
 * transition counts: a node already waiting in the first projection (or in a reconnect
 * snapshot that Studio has already seen) is not announced again, and one that stays
 * waiting is announced once.
 */
export function detectApprovalRequests(
  previous: ProjectionState | null | undefined,
  next: ProjectionState,
): RunNode[] {
  if (!previous) return []
  const found: RunNode[] = []
  for (const execId of next.runOrder) {
    if (isCompileRun(execId)) continue
    const run = next.runs[execId]
    if (!run) continue
    for (const node of Object.values(run.nodes)) {
      if (node.status !== 'waiting' || node.waiting?.kind !== 'human') continue
      const before = previous.runs[execId]?.nodes[node.nodeId]
      if (before?.status === 'waiting' && before.waiting?.kind === 'human') continue
      found.push(node)
    }
  }
  return found
}

export interface RunProduct {
  /** Page to show in the Preview panel, relative to the run's src folder. */
  previewEntry?: string
  /** Files worth opening, relative to src, best first. */
  codeFiles: string[]
}

const IGNORED_SEGMENTS = new Set(['.rag', '.git', 'node_modules', '__pycache__', 'dist', 'build'])
const LOCKFILES = new Set(['package-lock.json', 'yarn.lock', 'pnpm-lock.yaml', 'poetry.lock', 'uv.lock'])
const CODE_EXTENSIONS = new Set([
  'html', 'css', 'js', 'mjs', 'jsx', 'ts', 'tsx', 'vue', 'svelte', 'py', 'go', 'rs',
  'java', 'rb', 'php', 'sh', 'json', 'yml', 'yaml', 'toml', 'md', 'sql', 'svg',
])

function normalize(path: string): string {
  return path.replace(/\\/g, '/').replace(/^\.?\//, '')
}

function extensionOf(path: string): string {
  const name = path.slice(path.lastIndexOf('/') + 1)
  const dot = name.lastIndexOf('.')
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : ''
}

function depthThenName(a: string, b: string): number {
  return a.split('/').length - b.split('/').length || a.localeCompare(b)
}

/** Pick the page to preview and the files to open from a run's output paths. */
export function chooseProduct(paths: readonly string[]): RunProduct {
  const files = [...new Set(paths.map(normalize))].filter((path) => {
    const segments = path.split('/')
    return (
      path !== '' &&
      !segments.some((segment) => IGNORED_SEGMENTS.has(segment)) &&
      !LOCKFILES.has(segments[segments.length - 1])
    )
  })
  const pages = files.filter((path) => extensionOf(path) === 'html').sort(depthThenName)
  const previewEntry =
    pages.find((path) => path === 'index.html') ??
    pages.find((path) => path.endsWith('/index.html')) ??
    pages[0]
  const code = files.filter((path) => CODE_EXTENSIONS.has(extensionOf(path))).sort(depthThenName)
  const codeFiles = previewEntry ? [previewEntry, ...code.filter((path) => path !== previewEntry)] : code
  return { previewEntry, codeFiles }
}

/** "5m 3s", "42s", "1h 2m" — for the toast title. */
export function formatRunDuration(ms: number): string {
  const seconds = Math.max(0, Math.round(ms / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`
}
