import { useEffect, useState } from 'react'
import { FileCode2, Package, RefreshCw } from 'lucide-react'
import { cn } from '@/design/cn'
import { EmptyState, IconButton, SectionLabel, Spinner } from '@/design/primitives'
import { formatBytes } from '@/design/status'
import { bridge } from '@/state/bridge'
import { useActiveRun, useStudio } from '@/state/store'
import { useUi } from '@/state/ui'
import type { OutputFile } from '@shared/ipc'

/**
 * Artifacts and generated files for the active run.
 *
 * Two sources, deliberately kept apart because they mean different things:
 *  - artifacts, derived from the event stream (what agents claimed to produce)
 *  - outputs, from `GET /api/outputs/{execId}` (what actually landed on disk
 *    under .reticle/sessions/{execId}/src)
 */
export function ArtifactsSidebar() {
  const run = useActiveRun()
  const selectNode = useStudio((s) => s.selectNode)
  const setView = useUi((s) => s.setView)
  const openTab = useUi((s) => s.openTab)

  const [outputs, setOutputs] = useState<OutputFile[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = async () => {
    if (!bridge || !run) return
    setLoading(true)
    setError(null)
    const result = await bridge.api.outputs(run.execId)
    setLoading(false)
    if (result.ok && result.data) setOutputs(result.data)
    else setError(result.error ?? 'Could not read outputs.')
  }

  useEffect(() => {
    setOutputs(null)
    setError(null)
    if (run) void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run?.execId])

  if (!run) {
    return (
      <EmptyState
        icon={<Package size={20} strokeWidth={1.5} />}
        title="No run selected"
        description="Artifacts appear here once a run produces them."
      />
    )
  }

  const producers = Object.values(run.nodes).filter((node) => node.artifacts.length > 0)

  return (
    <div className="flex h-full min-h-0 flex-col overflow-y-auto">
      <section>
        <SectionLabel>
          Artifacts
          <span className="num ml-auto text-fg-4 normal-case">
            {producers.reduce((total, node) => total + node.artifacts.length, 0)}
          </span>
        </SectionLabel>
        {producers.length === 0 ? (
          <p className="px-3 py-2 text-2xs text-fg-4">
            No artifacts recorded for {run.execId} yet.
          </p>
        ) : (
          producers.map((node) => (
            <div key={node.nodeId}>
              <button
                type="button"
                onClick={() => {
                  selectNode(node.nodeId)
                  setView('graph')
                }}
                className="mono flex w-full items-center gap-1.5 px-3 pt-1.5 pb-0.5 text-left text-2xs text-fg-4 hover:text-fg-2"
              >
                {node.label}
              </button>
              {node.artifacts.map((artifact) => (
                <div
                  key={`${artifact.id}-${artifact.version ?? 0}`}
                  className="flex items-center gap-2 px-3 py-1 pl-5"
                >
                  <Package size={10} strokeWidth={1.9} className="shrink-0 text-fg-4" />
                  <span
                    className="truncate-1 min-w-0 flex-1 text-xs text-fg-2"
                    title={artifact.id}
                  >
                    {artifact.name ?? artifact.id}
                  </span>
                  {artifact.type ? (
                    <span className="shrink-0 text-[10px] text-fg-4">{artifact.type}</span>
                  ) : null}
                  <span className="num shrink-0 text-[10px] text-fg-4">
                    {formatBytes(artifact.dataSize)}
                  </span>
                </div>
              ))}
            </div>
          ))
        )}
      </section>

      <section>
        <SectionLabel>
          Generated files
          <IconButton
            label="Reload outputs"
            size="sm"
            className="ml-auto"
            onClick={load}
            disabled={loading}
          >
            {loading ? <Spinner size={10} /> : <RefreshCw size={11} strokeWidth={1.9} />}
          </IconButton>
        </SectionLabel>

        {error ? (
          <p className="px-3 py-2 text-2xs text-st-failed">{error}</p>
        ) : outputs === null ? (
          <p className="px-3 py-2 text-2xs text-fg-4">Loading…</p>
        ) : outputs.length === 0 ? (
          <p className="pretty px-3 py-2 text-2xs text-fg-4">
            Nothing on disk under <span className="mono">sessions/{run.execId}/src</span>{' '}
            yet.
          </p>
        ) : (
          outputs.map((file) => (
            <button
              key={file.path}
              type="button"
              onClick={() =>
                openTab({
                  id: `output:${run.execId}:${file.path}`,
                  kind: 'file',
                  title: file.path.split('/').pop() ?? file.path,
                  subtitle: file.path,
                  // Paths from /api/outputs are relative to the session's src/,
                  // which main cannot safely resolve — the endpoint already
                  // inlines the content, so carry it on the tab.
                  content: file.content,
                })
              }
              className={cn(
                'flex h-[var(--h-tree-row)] w-full items-center gap-2 px-3 text-left',
                '[transition-property:background-color] duration-[var(--dur-fast)]',
                'hover:bg-bg-2',
              )}
              title={file.path}
            >
              <FileCode2 size={11} strokeWidth={1.8} className="shrink-0 text-fg-4" />
              <span className="mono truncate-1 min-w-0 flex-1 text-2xs text-fg-2">
                {file.path}
              </span>
              <span className="num shrink-0 text-[10px] text-fg-4">
                {formatBytes(file.content.length)}
              </span>
            </button>
          ))
        )}
      </section>
    </div>
  )
}
