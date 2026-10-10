import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { ChevronRight, FileCode2, ShieldAlert } from 'lucide-react'
import { cn } from '@/design/cn'
import { Badge, Chip, Spinner } from '@/design/primitives'
import { NODE_STATUS_LABEL, NODE_STATUS_VAR } from '@/design/status'
import { openRunFile } from '@/state/actions'
import { bridge } from '@/state/bridge'
import type { AgentCard, OutputFile } from '@shared/ipc'
import type { Run, RunNode } from '@shared/projection'
import { Markdown } from './Markdown'
import { buildPlan, parseCheckpoint } from './planModel'
import type { CheckpointInput } from './planModel'

const PREVIEW_EXTENSIONS = /\.(md|markdown|json|txt)$/i
const MAX_PREVIEWS = 6
const PREVIEW_CHARS = 20_000

/** Capabilities a reviewer should notice before approving what runs next. */
const SENSITIVE = new Set(['process.native', 'process.container', 'network.public', 'cloud.apply', 'security.active', 'workspace.write', 'mcp.call'])

function cardFor(node: RunNode, cards: AgentCard[] | null): AgentCard | undefined {
  if (!cards || !node.agentId) return undefined
  // A generated agent runs as <execution>__<name>.
  const name = node.agentId.split('__').pop()
  return cards.find((card) => card.id === node.agentId) ?? cards.find((card) => card.id === name)
}

function Section({ title, hint, children, open = true }: { title: string; hint?: string; children: ReactNode; open?: boolean }) {
  return (
    <details open={open} className="group border-b border-line-1 last:border-b-0">
      <summary className="flex cursor-pointer list-none items-center gap-1.5 px-4 py-2 select-none hover:bg-bg-2">
        <ChevronRight size={12} strokeWidth={1.8} className="shrink-0 text-fg-4 transition-transform group-open:rotate-90" />
        <span className="text-2xs font-semibold tracking-[0.08em] text-fg-3 uppercase">{title}</span>
        {hint ? <span className="text-2xs text-fg-4">{hint}</span> : null}
      </summary>
      <div className="px-4 pb-3">{children}</div>
    </details>
  )
}

function DataBlock({ data }: { data: unknown }) {
  if (typeof data === 'string') return <Markdown source={data} />
  let text: string
  try {
    text = JSON.stringify(data, null, 2) ?? String(data)
  } catch {
    text = String(data)
  }
  return (
    <pre className="mono max-h-60 overflow-auto rounded-[var(--radius-control)] border border-line-1 bg-inset p-2 text-[11px] leading-relaxed whitespace-pre text-fg-2">
      {text.length > 6000 ? `${text.slice(0, 6000)}\n… (${text.length - 6000} more characters)` : text}
    </pre>
  )
}

function FilePreview({ file }: { file: OutputFile }) {
  const content = file.content.slice(0, PREVIEW_CHARS)
  const cut = file.content.length > PREVIEW_CHARS
  if (/\.(md|markdown|txt)$/i.test(file.path)) return <Markdown source={content + (cut ? '\n\n… shortened for display.' : '')} />
  let text = content
  try {
    if (!cut) text = JSON.stringify(JSON.parse(file.content), null, 2)
  } catch {
    // Not valid JSON on its own: show it as written.
  }
  return (
    <pre className="mono max-h-72 overflow-auto rounded-[var(--radius-control)] border border-line-1 bg-inset p-2 text-[11px] leading-relaxed whitespace-pre text-fg-2">
      {text}
      {cut ? '\n… shortened for display.' : ''}
    </pre>
  )
}

function UpstreamStep({ node, summaries, outputs }: { node: RunNode; summaries: CheckpointInput[]; outputs: OutputFile[] | null }) {
  const files = node.files
  const previews = useMemo(() => {
    if (!outputs) return []
    const byPath = new Map(outputs.map((file) => [file.path, file]))
    return files.filter((path) => PREVIEW_EXTENSIONS.test(path)).map((path) => byPath.get(path)).filter((file): file is OutputFile => file !== undefined).slice(0, MAX_PREVIEWS)
  }, [files, outputs])

  return (
    <div className="mb-3 rounded-[var(--radius-control)] border border-line-1 bg-bg-1">
      <div className="flex items-center gap-2 border-b border-line-1 px-3 py-1.5">
        <span className="text-xs font-medium text-fg-1">{node.label}</span>
        <Badge color={NODE_STATUS_VAR[node.status]}>{NODE_STATUS_LABEL[node.status]}</Badge>
      </div>
      <div className="px-3 py-2">
        {summaries.length > 0 ? summaries.map((input, index) => <DataBlock key={index} data={input.data} />) : <p className="text-2xs text-fg-4">This step left no summary.</p>}
        {files.length > 0 ? (
          <div className="mt-2">
            <div className="mb-1 text-2xs font-semibold tracking-wide text-fg-3 uppercase">Files written ({files.length})</div>
            <ul className="flex flex-col gap-0.5">
              {files.slice(0, 30).map((path) => (
                <li key={path}>
                  <button
                    type="button"
                    onClick={() => void openRunFile(node.execId, path)}
                    title={`Open ${path}`}
                    className="mono flex w-full items-center gap-1.5 truncate-1 rounded-[3px] px-1.5 py-0.5 text-left text-2xs text-fg-2 hover:bg-bg-3"
                  >
                    <FileCode2 size={11} strokeWidth={1.7} className="shrink-0 text-fg-4" />
                    {path}
                  </button>
                </li>
              ))}
              {files.length > 30 ? <li className="px-1.5 text-2xs text-fg-4">and {files.length - 30} more</li> : null}
            </ul>
          </div>
        ) : null}
        {previews.map((file) => (
          <details key={file.path} className="group mt-2 rounded-[var(--radius-control)] border border-line-1">
            <summary className="mono cursor-pointer list-none px-2 py-1 text-2xs text-fg-2 select-none hover:bg-bg-2">
              <ChevronRight size={11} strokeWidth={1.8} className="mr-1 inline text-fg-4 transition-transform group-open:rotate-90" />
              {file.path}
            </summary>
            <div className="px-2 pb-2">
              <FilePreview file={file} />
            </div>
          </details>
        ))}
      </div>
    </div>
  )
}

function NextStep({ node, card }: { node: RunNode; card?: AgentCard }) {
  const sensitive = card?.capabilities.filter((capability) => SENSITIVE.has(capability)) ?? []
  const other = card?.capabilities.filter((capability) => !SENSITIVE.has(capability)) ?? []
  return (
    <li className="rounded-[var(--radius-control)] border border-line-1 bg-bg-1 px-3 py-2">
      <div className="flex items-center gap-2">
        <span className="text-xs font-medium text-fg-1">{node.label}</span>
        {node.agentId && node.agentId !== node.label ? <Chip>{node.agentId}</Chip> : null}
      </div>
      {card?.description ? <p className="pretty mt-1 text-2xs leading-relaxed text-fg-3">{card.description}</p> : null}
      {card && card.capabilities.length > 0 ? (
        <div className="mt-1.5 flex flex-wrap gap-1">
          {sensitive.map((capability) => (
            <Chip key={capability} className="border-st-waiting/50 text-st-waiting" title="This step can use this capability">
              {capability}
            </Chip>
          ))}
          {other.map((capability) => (
            <Chip key={capability}>{capability}</Chip>
          ))}
        </div>
      ) : null}
    </li>
  )
}

/**
 * The plan behind an approval request: what was asked, what the previous step produced, and
 * what will run once you approve. Built from the run projection, the agent manifests and the
 * run's output files; the request file itself is only read for the goal and the step summary.
 */
export function PlanView({ run, node, checkpointText }: { run: Run; node: RunNode; checkpointText: string | null }) {
  const [cards, setCards] = useState<AgentCard[] | null>(null)
  const [outputs, setOutputs] = useState<OutputFile[] | null>(null)

  useEffect(() => {
    if (!bridge) return
    let cancelled = false
    void bridge.workspace.agents(run.execId).then((result) => {
      if (!cancelled && result.ok) setCards(result.data ?? [])
    })
    void bridge.api.outputs(run.execId).then((result) => {
      if (!cancelled) setOutputs(result.ok ? (result.data ?? []) : [])
    })
    return () => {
      cancelled = true
    }
  }, [run.execId])

  const checkpoint = useMemo(() => (checkpointText === null ? null : parseCheckpoint(checkpointText)), [checkpointText])
  const plan = useMemo(() => buildPlan(run, node.nodeId), [run, node.nodeId])

  if (checkpoint === null) {
    return (
      <div className="flex items-center gap-2 px-4 py-3 text-xs text-fg-3">
        <Spinner /> Reading the request…
      </div>
    )
  }

  // An input belongs to the upstream step whose task id prefixes its artifact id.
  const claimed = new Set<CheckpointInput>()
  const summariesFor = (step: RunNode) => {
    const matches = checkpoint.inputs.filter((input) => input.artifactId?.startsWith(step.taskId))
    matches.forEach((input) => claimed.add(input))
    return matches
  }
  const upstream = plan.upstream.map((step) => ({ step, summaries: summariesFor(step) }))
  const unmatched = checkpoint.inputs.filter((input) => !claimed.has(input))
  const stepCount = plan.waves.reduce((sum, wave) => sum + wave.length, 0) + plan.hidden

  return (
    <div>
      {checkpoint.prompt ? (
        <Section title="Goal">
          <p className="pretty text-xs leading-relaxed text-fg-1">{checkpoint.prompt}</p>
        </Section>
      ) : null}

      {checkpoint.protectedAction !== undefined ? (
        <Section title="Protected action">
          <div className="flex gap-2 rounded-[var(--radius-control)] border border-st-waiting/50 bg-st-waiting-weak p-2">
            <ShieldAlert size={14} strokeWidth={1.8} className="mt-0.5 shrink-0 text-st-waiting" />
            <div className="min-w-0 flex-1">
              <DataBlock data={checkpoint.protectedAction} />
            </div>
          </div>
        </Section>
      ) : null}

      <Section title="Result so far" hint={plan.upstream.length > 0 ? `${plan.upstream.length} step${plan.upstream.length === 1 ? '' : 's'} before this checkpoint` : undefined}>
        {upstream.map(({ step, summaries }) => (
          <UpstreamStep key={step.nodeId} node={step} summaries={summaries} outputs={outputs} />
        ))}
        {unmatched.map((input, index) => (
          <div key={index} className="mb-3 rounded-[var(--radius-control)] border border-line-1 bg-bg-1 px-3 py-2">
            {input.name ? <div className="mb-1 text-xs font-medium text-fg-1">{input.name}</div> : null}
            <DataBlock data={input.data} />
          </div>
        ))}
        {upstream.length === 0 && unmatched.length === 0 ? <p className="text-2xs text-fg-4">No earlier step fed this checkpoint.</p> : null}
        {!checkpoint.parsed ? <p className="mt-1 text-2xs text-fg-4">The request could not be read as structured data; see the raw request below.</p> : null}
      </Section>

      <Section title="What runs after you approve" hint={stepCount > 0 ? `${stepCount} step${stepCount === 1 ? '' : 's'}` : undefined}>
        {plan.waves.length === 0 ? (
          <p className="text-2xs text-fg-4">Nothing is scheduled after this checkpoint. Approving completes the run.</p>
        ) : (
          <ol className="space-y-3">
            {plan.waves.map((wave, index) => (
              <li key={index}>
                <div className="mb-1 text-2xs text-fg-4">
                  Step {index + 1}
                  {wave.length > 1 ? ` · ${wave.length} run in parallel` : ''}
                </div>
                <ul className="space-y-1.5">
                  {wave.map((step) => (
                    <NextStep key={step.nodeId} node={step} card={cardFor(step, cards)} />
                  ))}
                </ul>
              </li>
            ))}
          </ol>
        )}
        {plan.hidden > 0 ? <p className="mt-2 text-2xs text-fg-4">and {plan.hidden} more steps not listed.</p> : null}
      </Section>

      <Section title="Whole workflow" open={false}>
        <ol className="space-y-0.5">
          {plan.outline.map(({ node: step, depth, here }) => (
            <li key={step.nodeId} className="flex items-center gap-2 text-xs" style={{ paddingLeft: `${Math.min(depth, 8) * 12}px` }}>
              <span className={cn('inline-block h-1.5 w-1.5 shrink-0 rounded-full')} style={{ backgroundColor: here ? 'var(--color-st-waiting)' : NODE_STATUS_VAR[step.status] }} />
              <span className={cn('truncate-1', here ? 'font-semibold text-fg-1' : 'text-fg-2')}>{step.label}</span>
              {here ? <span className="text-2xs text-st-waiting">this checkpoint</span> : <span className="text-2xs text-fg-4">{NODE_STATUS_LABEL[step.status]}</span>}
            </li>
          ))}
        </ol>
      </Section>
    </div>
  )
}
