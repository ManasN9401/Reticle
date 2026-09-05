import { useVirtualizer } from '@tanstack/react-virtual'
import { ArrowDownToLine, Ban, Filter, Search, Trash2 } from 'lucide-react'
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { cn } from '@/design/cn'
import { EmptyState, IconButton, Input } from '@/design/primitives'
import { formatClock } from '@/design/status'
import { compactToolLog } from '@/features/graph/toolLog'
import { useActiveRun, useStudio } from '@/state/store'
import type { LogRecord } from '@shared/ipc'

const ROW_HEIGHT = 18

const LEVEL_COLOR: Record<LogRecord['level'], string> = {
  info: 'var(--color-fg-4)',
  warn: 'var(--color-st-waiting)',
  error: 'var(--color-st-failed)',
}

const LEVELS: LogRecord['level'][] = ['info', 'warn', 'error']

/**
 * The log stream.
 *
 * Virtualized without exception: `WorkerLog` is 77% of all runtime events
 * (61k of ~79k lines in a single 40MB runtime.log), so a naive list stops being
 * interactive within seconds of a real run.
 */
export function LogPanel() {
  const logs = useStudio((s) => s.logs)
  const clearLogs = useStudio((s) => s.clearLogs)
  const selectedNodeId = useStudio((s) => s.selectedNodeId)
  const followDefault = useStudio((s) => s.settings?.logs.followTail ?? true)
  const run = useActiveRun()

  const [search, setSearch] = useState('')
  const [levels, setLevels] = useState<Set<LogRecord['level']>>(new Set(LEVELS))
  const [scopeToNode, setScopeToNode] = useState(false)
  // Seeded from the persisted preference; scrolling away still releases it.
  const [follow, setFollow] = useState(followDefault)

  const scrollRef = useRef<HTMLDivElement>(null)

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase()
    return logs.filter((record) => {
      if (!levels.has(record.level)) return false
      if (run && record.execId && record.execId !== run.execId) return false
      if (scopeToNode && selectedNodeId && record.nodeId !== selectedNodeId) return false
      if (needle && !record.message.toLowerCase().includes(needle)) return false
      return true
    })
  }, [logs, levels, search, scopeToNode, selectedNodeId, run])

  const virtualizer = useVirtualizer({
    count: filtered.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 24,
  })

  // Stay pinned to the tail while following. Layout effect so the jump happens
  // in the same frame as the new rows, never as a visible scroll.
  useLayoutEffect(() => {
    if (!follow || filtered.length === 0) return
    virtualizer.scrollToIndex(filtered.length - 1, { align: 'end' })
  }, [follow, filtered.length, virtualizer])

  // Scrolling away from the bottom releases follow; scrolling back re-arms it.
  useEffect(() => {
    const element = scrollRef.current
    if (!element) return
    const onScroll = () => {
      const distance = element.scrollHeight - element.scrollTop - element.clientHeight
      setFollow(distance < ROW_HEIGHT * 2)
    }
    element.addEventListener('scroll', onScroll, { passive: true })
    return () => element.removeEventListener('scroll', onScroll)
  }, [])

  const items = virtualizer.getVirtualItems()

  return (
    <div className="flex h-full min-h-0 flex-col bg-inset">
      <div className="flex h-8 shrink-0 items-center gap-1.5 border-b border-line-1 bg-bg-1 px-2">
        <div className="relative w-64">
          <Search
            size={12}
            strokeWidth={1.8}
            className="pointer-events-none absolute top-1/2 left-2 -translate-y-1/2 text-fg-4"
          />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Filter logs"
            aria-label="Filter logs"
            className="h-6 w-full pl-6 text-xs"
          />
        </div>

        <div className="flex items-center gap-px rounded-[var(--radius-control)] border border-line-2 p-px">
          {LEVELS.map((level) => {
            const active = levels.has(level)
            return (
              <button
                key={level}
                type="button"
                aria-pressed={active}
                title={`${active ? 'Hide' : 'Show'} ${level} records`}
                onClick={() =>
                  setLevels((prev) => {
                    const next = new Set(prev)
                    if (next.has(level)) next.delete(level)
                    else next.add(level)
                    return next
                  })
                }
                className={cn(
                  'flex h-[18px] items-center gap-1 rounded-[3px] px-1.5 text-2xs uppercase',
                  '[transition-property:background-color,color,opacity] duration-[var(--dur-fast)]',
                  active ? 'bg-bg-3 text-fg-2' : 'text-fg-4 opacity-60 hover:opacity-100',
                )}
              >
                <span
                  className="h-1.5 w-1.5 rounded-full"
                  style={{ backgroundColor: LEVEL_COLOR[level] }}
                />
                {level}
              </button>
            )
          })}
        </div>

        <IconButton
          label={
            selectedNodeId
              ? `Scope to ${selectedNodeId}`
              : 'Select a node to scope the log to it'
          }
          size="sm"
          active={scopeToNode}
          disabled={!selectedNodeId}
          onClick={() => setScopeToNode((v) => !v)}
        >
          <Filter size={13} strokeWidth={1.7} />
        </IconButton>

        <span className="num ml-auto text-2xs text-fg-4">
          {filtered.length.toLocaleString()}
          {filtered.length !== logs.length ? ` / ${logs.length.toLocaleString()}` : ''}
        </span>

        <IconButton
          label={follow ? 'Following tail' : 'Follow tail'}
          size="sm"
          active={follow}
          onClick={() => {
            setFollow(true)
            if (filtered.length > 0) {
              virtualizer.scrollToIndex(filtered.length - 1, { align: 'end' })
            }
          }}
        >
          <ArrowDownToLine size={13} strokeWidth={1.7} />
        </IconButton>

        <IconButton label="Clear logs" size="sm" onClick={clearLogs}>
          <Trash2 size={13} strokeWidth={1.7} />
        </IconButton>
      </div>

      <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto">
        {filtered.length === 0 ? (
          <EmptyState
            icon={<Ban size={20} strokeWidth={1.5} />}
            title={logs.length === 0 ? 'No log output yet' : 'No records match the filter'}
            description={
              logs.length === 0
                ? 'Worker output appears here as soon as a run starts producing events.'
                : undefined
            }
          />
        ) : (
          <div
            className="relative w-full"
            style={{ height: virtualizer.getTotalSize() }}
          >
            <div
              className="absolute inset-x-0 top-0"
              style={{ transform: `translateY(${items[0]?.start ?? 0}px)` }}
            >
              {items.map((item) => (
                <LogRow
                  key={item.key}
                  record={filtered[item.index]}
                  height={ROW_HEIGHT}
                />
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

function LogRow({ record, height }: { record: LogRecord; height: number }) {
  return (
    <div
      className="mono flex items-center gap-2 px-2 text-code leading-none hover:bg-bg-1"
      style={{ height }}
    >
      <span className="num shrink-0 text-fg-4">{formatClock(record.at)}</span>
      <span
        className="h-1.5 w-1.5 shrink-0 rounded-full"
        style={{ backgroundColor: LEVEL_COLOR[record.level] }}
        title={record.level}
      />
      {record.agentId ? (
        <span className="w-36 shrink-0 truncate-1 text-fg-3" title={record.agentId}>
          {record.agentId}
        </span>
      ) : (
        <span className="w-36 shrink-0" />
      )}
      <span
        className={cn(
          'truncate-1 min-w-0 flex-1',
          record.level === 'error' ? 'text-st-failed' : 'text-fg-2',
          record.isLlm && 'text-fg-3 italic',
        )}
        title={record.message}
      >
        {compactToolLog(record.message)}
      </span>
    </div>
  )
}
