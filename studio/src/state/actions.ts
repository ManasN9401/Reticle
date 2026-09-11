import type { Attachment } from '@shared/events'
import type { HitlResolveRequest, OutboundCommand } from '@shared/ipc'
import { bridge } from './bridge'
import { useStudio } from './store'
import { useUi } from './ui'

/**
 * Every user-triggered action in one place.
 *
 * The rule for this build is that a visible control is either backed by
 * something real or is visibly disabled — so each function here maps to a
 * concrete capability of the orchestrator, and the `can*` guards below are what
 * the UI uses to disable rather than silently no-op.
 */

export interface EnqueueOptions {
  prompt: string
  mode?: 'parallel' | 'sequential'
  group?: string
  effort?: string
  ideContext?: string
  attachments?: Attachment[]
}

export async function connect(): Promise<void> {
  if (!bridge) return
  const settings = useStudio.getState().settings
  const host = settings?.connection.host ?? '127.0.0.1'
  const port = settings?.connection.port ?? 8080
  await bridge.connection.connect({ host, port })
}

export async function disconnect(): Promise<void> {
  await bridge?.connection.disconnect()
}

export async function startForge(prompt?: string): Promise<void> {
  if (!bridge) return
  const settings = useStudio.getState().settings
  await bridge.forge.start({
    port: settings?.connection.port ?? 8080,
    batch: settings?.forge.batch,
    retries: settings?.forge.retries,
    isolated: settings?.forge.isolated,
    allModels: settings?.forge.allModels,
    workspace: settings?.forge.workspace,
    prompt,
  })
}

export async function stopForge(): Promise<void> {
  await bridge?.forge.stop()
}

/**
 * Submit a prompt. The socket accepts only `enqueue` and `remove`
 * (runtime/telemetry/server.go:250), so this is the single write path into the
 * orchestrator.
 */
export async function enqueue(options: EnqueueOptions): Promise<boolean> {
  if (!bridge) return false
  const prompt = options.prompt.trim()
  if (!prompt) return false

  const command: OutboundCommand = {
    action: 'enqueue',
    prompt,
    group: options.group ?? '',
    mode: options.mode ?? 'parallel',
    effort: options.effort ?? 'auto',
    ide_context: options.ideContext,
    attachments: options.attachments,
  }
  return bridge.connection.send(command)
}

export async function removeFromQueue(id: string): Promise<boolean> {
  if (!bridge) return false
  return bridge.connection.send({ action: 'remove', id })
}

export async function killRun(id: string): Promise<boolean> {
  if (!bridge) return false
  return bridge.connection.send({ action: 'kill', id })
}

export async function pauseRun(id: string): Promise<boolean> {
  if (!bridge) return false
  return bridge.connection.send({ action: 'pause', id })
}

export async function resumeRun(id: string): Promise<boolean> {
  if (!bridge) return false
  return bridge.connection.send({ action: 'resume', id })
}

export async function uploadAttachments() {
  if (!bridge) return { ok: false as const, error: 'bridge unavailable' }
  const paths = await bridge.workspace.pickFiles()
  if (paths.length === 0) return { ok: true as const, data: [] }
  return bridge.api.upload(paths)
}

export async function resolveApproval(request: HitlResolveRequest) {
  if (!bridge) return { ok: false as const, error: 'bridge unavailable' }
  const result = await bridge.hitl.resolve(request)
  if (result.ok) useUi.getState().setReviewNode(null)
  return result
}

export async function openFileInEditor(path: string, title?: string): Promise<void> {
  useUi.getState().openTab({
    id: `file:${path}`,
    kind: 'file',
    title: title ?? path.split(/[\\/]/).pop() ?? path,
    path,
    subtitle: path,
  })
}

export async function revealInExplorer(path: string): Promise<void> {
  await bridge?.workspace.reveal(path)
}

// ---------------------------------------------------------------------------
// Capability guards — what the UI disables against
// ---------------------------------------------------------------------------

export function canSubmit(): boolean {
  return useStudio.getState().connection.phase === 'connected'
}

export function canStartForge(): boolean {
  const { forge, settings } = useStudio.getState()
  if (!settings?.forge.binaryPath) return false
  return forge.phase === 'stopped' || forge.phase === 'exited' || forge.phase === 'error'
}

export function canStopForge(): boolean {
  const phase = useStudio.getState().forge.phase
  return phase === 'running' || phase === 'starting'
}
