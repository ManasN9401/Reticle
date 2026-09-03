import { useEffect, useMemo, useState } from 'react'
import { Boxes, CircleDot, Search } from 'lucide-react'
import { cn } from '@/design/cn'
import { Chip, EmptyState, Input, SectionLabel, Spinner } from '@/design/primitives'
import { bridge } from '@/state/bridge'
import { openFileInEditor } from '@/state/actions'
import { useActiveRun } from '@/state/store'
import type { AgentCard } from '@shared/ipc'

/**
 * Agent roster.
 *
 * Reads both manifest conventions that coexist in the repo (`definition.yml`
 * and `<name>.yaml`) and shows fields the Go loader silently drops
 * (`capabilities`, `required_skills`) — the card should reflect what is written
 * down, not only what `AgentDefinition` happens to parse.
 */
export function AgentsSidebar() {
  const run = useActiveRun()
  const [agents, setAgents] = useState<AgentCard[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [expanded, setExpanded] = useState<string | null>(null)

  useEffect(() => {
    if (!bridge) return
    let cancelled = false
    setAgents(null)
    void bridge.workspace.agents(run?.execId).then((result) => {
      if (cancelled) return
      if (result.ok && result.data) setAgents(result.data)
      else setError(result.error ?? 'Could not read the agent catalog.')
    })
    return () => {
      cancelled = true
    }
  }, [run?.execId])

  const filtered = useMemo(() => {
    if (!agents) return []
    const needle = search.trim().toLowerCase()
    if (!needle) return agents
    return agents.filter(
      (agent) =>
        agent.id.toLowerCase().includes(needle) ||
        agent.name?.toLowerCase().includes(needle) ||
        agent.description?.toLowerCase().includes(needle),
    )
  }, [agents, search])

  const session = filtered.filter((a) => a.origin === 'session')
  const builtin = filtered.filter((a) => a.origin === 'builtin')

  if (error) {
    return <EmptyState title="Agent catalog unavailable" description={error} />
  }

  if (agents === null) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-xs text-fg-3">
        <Spinner /> Reading agents…
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="relative shrink-0 p-2">
        <Search
          size={12}
          strokeWidth={1.8}
          className="pointer-events-none absolute top-1/2 left-4 -translate-y-1/2 text-fg-4"
        />
        <Input
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder="Filter agents"
          aria-label="Filter agents"
          className="h-7 pl-7 text-xs"
        />
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto pb-2">
        {filtered.length === 0 ? (
          <EmptyState
            icon={<Boxes size={20} strokeWidth={1.5} />}
            title="No agents match"
          />
        ) : null}

        {session.length > 0 ? (
          <section>
            <SectionLabel>
              Generated for this run
              <span className="num ml-auto text-fg-4 normal-case">{session.length}</span>
            </SectionLabel>
            {session.map((agent) => (
              <AgentRow
                key={`session-${agent.id}`}
                agent={agent}
                expanded={expanded === `session-${agent.id}`}
                onToggle={() =>
                  setExpanded((prev) =>
                    prev === `session-${agent.id}` ? null : `session-${agent.id}`,
                  )
                }
              />
            ))}
          </section>
        ) : null}

        {builtin.length > 0 ? (
          <section>
            <SectionLabel>
              Catalog
              <span className="num ml-auto text-fg-4 normal-case">{builtin.length}</span>
            </SectionLabel>
            {builtin.map((agent) => (
              <AgentRow
                key={`builtin-${agent.id}`}
                agent={agent}
                expanded={expanded === `builtin-${agent.id}`}
                onToggle={() =>
                  setExpanded((prev) =>
                    prev === `builtin-${agent.id}` ? null : `builtin-${agent.id}`,
                  )
                }
              />
            ))}
          </section>
        ) : null}
      </div>
    </div>
  )
}

function AgentRow({
  agent,
  expanded,
  onToggle,
}: {
  agent: AgentCard
  expanded: boolean
  onToggle: () => void
}) {
  return (
    <div className={cn(expanded && 'bg-bg-2')}>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className={cn(
          'flex w-full items-center gap-2 px-3 py-1.5 text-left',
          '[transition-property:background-color] duration-[var(--dur-fast)]',
          !expanded && 'hover:bg-bg-2',
        )}
      >
        <CircleDot
          size={11}
          strokeWidth={2}
          className={agent.hasEnv ? 'shrink-0 text-st-done' : 'shrink-0 text-fg-4'}
        />
        <span className="min-w-0 flex-1">
          <span className="truncate-1 block text-xs text-fg-1">
            {agent.name ?? agent.id}
          </span>
          <span className="mono truncate-1 block text-2xs text-fg-4">{agent.id}</span>
        </span>
        {agent.runtime ? (
          <span className="shrink-0 text-[10px] text-fg-4">{agent.runtime}</span>
        ) : null}
      </button>

      {expanded ? (
        <div className="border-t border-line-1 px-3 py-2">
          {agent.description ? (
            <p className="pretty mb-2 text-2xs leading-relaxed text-fg-3">
              {agent.description}
            </p>
          ) : null}

          <MetaList label="Capabilities" values={agent.capabilities} />
          <MetaList label="Skills" values={agent.skills} />
          <MetaList label="Inputs" values={agent.inputs} />
          <MetaList label="Outputs" values={agent.outputs} />
          <MetaList label="Memory" values={agent.memory} />

          <div className="mt-2 flex items-center gap-2">
            <span
              className="text-[10px] text-fg-4"
              title={
                agent.hasEnv
                  ? 'A provisioned virtualenv exists under .reticle/envs'
                  : 'No virtualenv provisioned yet'
              }
            >
              {agent.hasEnv ? 'env ready' : 'no env'}
            </span>
            <button
              type="button"
              onClick={() => openFileInEditor(agent.manifestPath)}
              className="ml-auto text-[10px] text-accent hover:underline"
            >
              Open manifest
            </button>
          </div>
        </div>
      ) : null}
    </div>
  )
}

function MetaList({ label, values }: { label: string; values: string[] }) {
  if (values.length === 0) return null
  return (
    <div className="mb-1.5 flex items-baseline gap-2">
      <span className="w-16 shrink-0 text-[10px] text-fg-4">{label}</span>
      <span className="flex min-w-0 flex-wrap gap-1">
        {values.map((value) => (
          <Chip key={value}>{value}</Chip>
        ))}
      </span>
    </div>
  )
}
