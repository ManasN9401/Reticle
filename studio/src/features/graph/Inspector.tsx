import { useEffect, useMemo, useState } from 'react'
import { Download, FlaskConical, MousePointerSquareDashed, TriangleAlert } from 'lucide-react'
import { cn } from '@/design/cn'
import { Badge, Button, EmptyState, IconButton, StatusPip } from '@/design/primitives'
import {
  NODE_STATUS_LABEL,
  NODE_STATUS_VAR,
  formatBytes,
  formatDuration,
  formatFailureReason,
} from '@/design/status'
import { bridge } from '@/state/bridge'
import { useActiveRun, useActiveNode, useStudio } from '@/state/store'
import { useUi } from '@/state/ui'
import { compactToolLog } from './toolLog'
import { buildLlmDiagnostics, type LlmDiagnosticKind } from './llmLog'

type Tab = 'overview' | 'log' | 'llm'

/**
 * Node inspector.
 *
 * Covers RFC-022's law 6 — verbose `[TOOL]` JSON emitted by workers is compacted
 * into readable action descriptors rather than dumped raw — and keeps the LLM
 * diagnostics on their own tab so they do not drown the execution log.
 */
export function Inspector() {
  const run = useActiveRun()
  const node = useActiveNode()
  const logs = useStudio((s) => s.logs)
  const setReviewNode = useUi((s) => s.setReviewNode)
  const [tab, setTab] = useState<Tab>('overview')

  const nodeLogs = useMemo(
    () => (node ? logs.filter((r) => r.nodeId === node.nodeId) : []),
    [logs, node],
  )

  if (!node || !run) {
    return (
      <EmptyState
        icon={<MousePointerSquareDashed size={22} strokeWidth={1.4} />}
        title="No node selected"
        description="Click a node in the map to inspect its status, timings, artifacts and log."
      />
    )
  }

  const parents = run.edges.filter((e) => e.to === node.nodeId).map((e) => e.from)
  const children = run.edges.filter((e) => e.from === node.nodeId).map((e) => e.to)

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="shrink-0 border-b border-line-1 px-3 py-2.5">
        <div className="flex items-center gap-2">
          <StatusPip
            color={NODE_STATUS_VAR[node.status]}
            pulse={node.status === 'running'}
          />
          <span className="truncate-1 min-w-0 flex-1 text-sm font-medium text-fg-1">
            {node.label}
          </span>
          <Badge color={NODE_STATUS_VAR[node.status]}>
            {NODE_STATUS_LABEL[node.status]}
          </Badge>
          <IconButton
            label="Export logs for this node"
            size="sm"
            onClick={() =>
              bridge?.logs.export({
                query: { nodeId: node.nodeId },
                format: 'json',
                destination: 'file',
              })
            }
          >
            <Download size={13} strokeWidth={1.7} />
          </IconButton>
        </div>
        <div className="mono mt-1 truncate-1 text-2xs text-fg-4">{node.taskId}</div>
      </div>

      {node.status === 'waiting' && node.waiting?.kind === 'human' ? (
        <div className="shrink-0 border-b border-line-1 bg-st-waiting-weak px-3 py-2.5">
          <div className="text-xs font-medium text-st-waiting">Awaiting human approval</div>
          <p className="pretty mt-1 text-2xs text-fg-3">
            This node has suspended the branch and is polling its checkpoint file every 2
            seconds.
          </p>
          <Button
            size="sm"
            variant="primary"
            className="mt-2"
            onClick={() => setReviewNode(node.nodeId)}
          >
            Review checkpoint
          </Button>
        </div>
      ) : null}

      {node.failure ? (
        <div className="shrink-0 border-b border-line-1 bg-st-failed-weak px-3 py-2.5">
          <div className="flex items-center gap-1.5 text-xs font-medium text-st-failed">
            <TriangleAlert size={12} strokeWidth={1.9} />
            {formatFailureReason(node.failure.reason)}
            {node.failure.exitCode !== undefined ? (
              <span className="num text-fg-3">exit {node.failure.exitCode}</span>
            ) : null}
          </div>
          {node.failure.stderr ? (
            <pre className="mono mt-1.5 max-h-40 overflow-auto text-2xs whitespace-pre-wrap text-fg-3">
              {node.failure.stderr.split('\n').filter((l: string) => !l.trim().startsWith('[LLM_STREAM]')).join('\n').trim()}
            </pre>
          ) : null}
        </div>
      ) : null}

      {node.mocked ? (
        <div className="flex shrink-0 items-start gap-2 border-b border-line-1 bg-st-waiting-weak px-3 py-2">
          <FlaskConical size={12} strokeWidth={1.9} className="mt-0.5 text-st-waiting" />
          <p className="pretty text-2xs text-fg-2">
            This node fell back to <span className="text-st-waiting">mock output</span>. Its
            result is not real work.
          </p>
        </div>
      ) : null}

      <div className="flex shrink-0 items-stretch border-b border-line-1">
        {(
          [
            ['overview', 'Overview'],
            ['log', `Log${nodeLogs.length ? ` (${nodeLogs.length})` : ''}`],
            ['llm', 'LLM'],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            type="button"
            onClick={() => setTab(id)}
            className={cn(
              'relative px-3 py-1.5 text-xs',
              '[transition-property:color] duration-[var(--dur-fast)]',
              tab === id ? 'text-fg-1' : 'text-fg-3 hover:text-fg-2',
            )}
          >
            <span
              className={cn(
                'absolute inset-x-2 bottom-0 h-0.5 rounded-t-full bg-accent',
                '[transition-property:opacity] duration-[var(--dur-base)]',
                tab === id ? 'opacity-100' : 'opacity-0',
              )}
            />
            {label}
          </button>
        ))}
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {tab === 'overview' ? (
          <div className="flex flex-col">
            <Row label="Status" value={NODE_STATUS_LABEL[node.status]} />
            <Row
              label="Duration"
              value={formatDuration(node.durationMs)}
              hint={node.durationMs === undefined ? 'not finished' : 'derived'}
            />
            <Row label="Model" value={node.model ?? '—'} mono />
            <Row
              label="Attempts"
              value={String(node.attempts)}
              hint={node.attempts > 1 ? `${node.attempts - 1} retried` : undefined}
            />
            <Row label="Agent" value={node.agentId ?? '—'} mono />
            <Row label="Parents" value={parents.length ? parents.join(', ') : '—'} mono />
            <Row
              label="Children"
              value={children.length ? children.join(', ') : '—'}
              mono
            />

            <div className="border-t border-line-1 px-3 py-2">
              <div className="mb-1.5 text-2xs font-semibold tracking-wide text-fg-3 uppercase">
                Artifacts produced
              </div>
              {node.artifacts.length === 0 ? (
                <div className="text-2xs text-fg-4">None</div>
              ) : (
                <ul className="flex flex-col gap-1">
                  {node.artifacts.map((artifact) => (
                    <li
                      key={`${artifact.id}-${artifact.version ?? 0}`}
                      className="flex items-center gap-2 rounded-[var(--radius-control)] border border-line-1 bg-bg-2 px-2 py-1"
                    >
                      <span className="mono truncate-1 min-w-0 flex-1 text-2xs text-fg-2">
                        {artifact.name ?? artifact.id}
                      </span>
                      {artifact.type ? (
                        <span className="shrink-0 text-[10px] text-fg-4">
                          {artifact.type}
                        </span>
                      ) : null}
                      <span className="num shrink-0 text-[10px] text-fg-4">
                        {formatBytes(artifact.dataSize)}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
        ) : null}

        {tab === 'log' ? <NodeLog records={nodeLogs} llm={false} /> : null}
        {tab === 'llm' ? (
          <LlmNodeLog
            records={nodeLogs}
            model={node.model}
            attempts={node.attempts}
            startedAt={node.startedAt}
            running={node.status === 'running'}
          />
        ) : null}
      </div>
    </div>
  )
}

function Row({
  label,
  value,
  hint,
  mono,
}: {
  label: string
  value: string
  hint?: string
  mono?: boolean
}) {
  return (
    <div className="flex items-baseline gap-3 border-b border-line-1 px-3 py-1.5">
      <span className="w-20 shrink-0 text-2xs text-fg-4">{label}</span>
      <span
        className={cn('min-w-0 flex-1 truncate-1 text-xs text-fg-2', mono && 'mono')}
        title={value}
      >
        {value}
      </span>
      {hint ? <span className="shrink-0 text-[10px] text-fg-4">{hint}</span> : null}
    </div>
  )
}

function NodeLog({
  records,
  llm,
}: {
  records: { seq: number; message: string; isLlm: boolean; level: string }[]
  llm: boolean
}) {
  const filtered = records.filter((r) => r.isLlm === llm)
  if (filtered.length === 0) {
    return (
      <div className="px-3 py-6 text-center text-2xs text-fg-4">
        {llm
          ? 'Waiting for model output. Reasoning appears when the provider exposes it.'
          : 'No log output for this node.'}
      </div>
    )
  }
  if (llm) {
    const diagnostics = buildLlmDiagnostics(filtered)

    if (diagnostics.length === 0) {
      return (
        <div className="px-3 py-6 text-center text-2xs text-fg-4">
          No readable LLM diagnostics were recorded for this node.
        </div>
      )
    }

    return (
      <div className="flex flex-col gap-3 px-3 py-3 select-text">
        <p className="text-[10px] leading-relaxed text-fg-4">
          Reasoning is shown only when the selected model and provider return it.
        </p>
        {diagnostics.map((entry) => (
          <section
            key={`${entry.firstSeq}-${entry.lastSeq}-${entry.kind}`}
            className="overflow-hidden rounded-[var(--radius-control)] border border-line-1 bg-bg-2"
          >
            <div className="border-b border-line-1 px-2.5 py-1 text-[10px] font-semibold tracking-wide uppercase text-fg-4">
              {llmDiagnosticLabel(entry.kind, entry.name)}
            </div>
            <div
              className={cn(
                'px-2.5 py-2 text-xs leading-relaxed whitespace-pre-wrap break-words',
                entry.kind === 'reasoning' ? 'text-fg-3 italic' : 'text-fg-2',
                entry.kind === 'tool' && 'mono text-accent',
                entry.kind === 'status' && 'mono text-fg-3',
              )}
            >
              {entry.text}
            </div>
          </section>
        ))}
      </div>
    )
  }

  return (
    <div className="flex flex-col py-1">
      {filtered.map((record) => (
        <div
          key={record.seq}
          className={cn(
            'mono px-3 py-0.5 text-2xs leading-relaxed break-words whitespace-pre-wrap select-text',
            record.level === 'error' ? 'text-st-failed' : 'text-fg-3',
          )}
        >
          {compactToolLog(record.message)}
        </div>
      ))}
    </div>
  )
}

function LlmNodeLog({
  records,
  model,
  attempts,
  startedAt,
  running,
}: {
  records: { seq: number; message: string; isLlm: boolean; level: string }[]
  model?: string
  attempts: number
  startedAt?: number
  running: boolean
}) {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    if (!running) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [running])
  const elapsed = startedAt === undefined ? undefined : Math.max(0, now - startedAt)

  return (
    <div className="flex flex-col">
      <div className="border-b border-line-1 bg-bg-2 px-3 py-2 text-2xs text-fg-3">
        <div className="flex flex-wrap gap-x-3 gap-y-1">
          <span className="mono text-fg-2">{model ?? 'Model not selected'}</span>
          <span>Attempt {Math.max(1, attempts)}</span>
          {elapsed !== undefined ? <span>Elapsed {formatDuration(elapsed)}</span> : null}
        </div>
        <p className="mt-1 text-fg-4">
          Response tokens stream live. Separate reasoning appears only when the model returns a
          reasoning field.
        </p>
      </div>
      <NodeLog records={records} llm />
    </div>
  )
}

function llmDiagnosticLabel(kind: LlmDiagnosticKind, name?: string): string {
  if (kind === 'reasoning') return 'Provider reasoning'
  if (kind === 'content') return 'Response'
  if (kind === 'tool') return name ? `Tool request · ${name}` : 'Tool request'
  return 'Model status'
}
