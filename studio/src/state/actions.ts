import type { Attachment } from '@shared/events'
import { fileKindFor } from '@shared/fileKind'
import type { ArtifactRef } from '@shared/projection'
import type { HitlResolveRequest, OutboundCommand, OutputFile } from '@shared/ipc'
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
  agentComplexity?: number
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

/** Bring a node that is waiting for a decision into view and open its review. */
export function openApproval(execId: string, nodeId: string): void {
  useStudio.getState().selectRun(execId)
  useStudio.getState().selectNode(nodeId)
  const ui = useUi.getState()
  ui.setView('graph')
  ui.setReviewNode(nodeId)
}

/** Connect to a specific orchestrator address; the main process also saves it as the default. */
export async function connectTo(host: string, port: number): Promise<void> {
  await bridge?.connection.connect({ host, port })
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
    agent_complexity: options.agentComplexity ?? 3,
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

export async function updateSettings(settings: {
  num_ctx?: number
  max_tokens?: number
  temperature?: number
  use_bayesian_routing?: boolean
  first_token_timeout_seconds?: number
  ollama_keep_alive?: string
  allow_text_to_coding_fallback?: boolean
}): Promise<boolean> {
  if (!bridge) return false
  return bridge.connection.send({
    action: 'update_settings',
    ...settings,
  })
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

/** Remembered by the Preview panel so it can pick the URL up even when it mounts later. */
export const PREVIEW_URL_KEY = 'reticle.previewUrl'
/** Tabs opened for a finished run; the rest stay in the Artifacts view. */
const MAX_PRODUCT_TABS = 6

/** Serve a finished run's site and show it in the Preview panel. */
export async function openRunPreview(execId: string, entry?: string): Promise<boolean> {
  if (!bridge) return false
  const result = await bridge.preview.serveRun(execId, entry)
  if (!result.ok || !result.data) return false
  try {
    localStorage.setItem(PREVIEW_URL_KEY, result.data)
  } catch {
    // Storage can be unavailable; the event below still reaches a mounted panel.
  }
  useUi.getState().setPanelTab('preview')
  window.dispatchEvent(new CustomEvent('reticle-preview-url', { detail: result.data }))
  return true
}

/** Open a run's code files as editor tabs, leaving the first (best) one active. */
export function openRunFiles(execId: string, files: readonly OutputFile[], order: readonly string[]): void {
  const byPath = new Map(files.map((file) => [file.path, file]))
  const chosen = order
    .map((path) => byPath.get(path))
    .filter((file): file is OutputFile => file !== undefined)
    .slice(0, MAX_PRODUCT_TABS)
  const ui = useUi.getState()
  // Opening activates the tab, so go in reverse to end on the first one.
  for (const file of [...chosen].reverse()) {
    ui.openTab({
      id: `output:${execId}:${file.path}`,
      kind: 'file',
      title: file.path.split('/').pop() ?? file.path,
      subtitle: file.path,
      content: file.content,
    })
  }
  if (chosen.length < order.length) ui.setView('artifacts')
}

const NOT_PREVIEWABLE =
  'This file is too large, or not text, so it cannot be previewed here.\nOpen the run folder to see it on disk.'

/** The main process cuts artifact payloads to this many characters (electron/forge/store.ts). */
const ARTIFACT_PREVIEW_CHARS = 2_000

/**
 * Open one file a run produced. Images go to the image viewer, which streams them from
 * the run's preview server; everything else opens as text in the editor.
 */
export async function openRunFile(execId: string, path: string): Promise<void> {
  const ui = useUi.getState()
  const tab = {
    id: `output:${execId}:${path}`,
    kind: 'file' as const,
    title: path.split('/').pop() ?? path,
    subtitle: path,
    source: { execId, path },
  }
  if (fileKindFor(path) === 'image') {
    ui.openTab(tab)
    return
  }
  const outputs = bridge ? await bridge.api.outputs(execId) : undefined
  const match = outputs?.ok ? outputs.data?.find((file) => file.path === path) : undefined
  ui.openTab({ ...tab, content: match?.content ?? NOT_PREVIEWABLE })
}

/** Open the text of a node's summary artifact. */
export function openArtifactText(artifact: ArtifactRef): void {
  if (artifact.text === undefined) return
  const name = artifact.name ?? artifact.id
  const markdown = artifact.type?.endsWith('markdown') && !name.endsWith('.md')
  const truncated = artifact.text.length >= ARTIFACT_PREVIEW_CHARS
  useUi.getState().openTab({
    id: `artifact:${artifact.id}:${artifact.version ?? 0}`,
    kind: 'file',
    title: name,
    subtitle: markdown ? `${name}.md` : name,
    content: truncated ? `${artifact.text}

… (preview cut at ${ARTIFACT_PREVIEW_CHARS} characters)` : artifact.text,
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
