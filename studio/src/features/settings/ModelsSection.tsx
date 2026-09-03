import { useCallback, useEffect, useMemo, useState } from 'react'
import { AlertTriangle, KeyRound, RefreshCw, Search, X } from 'lucide-react'
import { cn } from '@/design/cn'
import { Button, EmptyState, IconButton, Input, Select, Spinner, Toggle } from '@/design/primitives'
import { bridge } from '@/state/bridge'
import { useStudio } from '@/state/store'
import { modelKey, type RoutingModel } from '@shared/events'
import type { EnvKeyEntry } from '@shared/ipc'

type StatusFilter = 'all' | 'enabled' | 'disabled'
type HealthFilter = 'all' | 'ok' | 'rate-limited' | 'missing-key'
type SortKey = 'capability' | 'cost' | 'id' | 'provider'

/** Model ids are `<provider>/<rest>`; the prefix is the routing provider. */
function providerOf(id: string): string {
  const slash = id.indexOf('/')
  return slash === -1 ? 'other' : id.slice(0, slash)
}

/**
 * Model catalog.
 *
 * The list is served live by the orchestrator (`GET /api/models`) and each
 * entry is bound to an API key slot, so the useful axes to slice by are the
 * provider, the key, and whether that key is currently usable — a model whose
 * key is rate-limited or absent from `.env` cannot run, and that is invisible
 * from the model id alone.
 */
export function ModelsSection() {
  const connected = useStudio((s) => s.connection.phase === 'connected')
  const lockedKeys = useStudio((s) => s.projection.waitlist?.lockedKeys ?? [])

  const [models, setModels] = useState<RoutingModel[] | null>(null)
  const [keys, setKeys] = useState<EnvKeyEntry[]>([])
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const [search, setSearch] = useState('')
  const [provider, setProvider] = useState('all')
  const [keyFilter, setKeyFilter] = useState('all')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [health, setHealth] = useState<HealthFilter>('all')
  const [sort, setSort] = useState<SortKey>('capability')

  const load = useCallback(async () => {
    if (!bridge) return
    setBusy(true)
    const [modelResult, keyResult] = await Promise.all([
      bridge.api.models(),
      bridge.keys.list(),
    ])
    setBusy(false)
    if (keyResult.ok && keyResult.data) setKeys(keyResult.data)
    if (modelResult.ok && modelResult.data) {
      setModels(modelResult.data)
      setError(null)
    } else {
      setError(modelResult.error ?? 'Could not read the model catalog.')
    }
  }, [])

  useEffect(() => {
    if (connected) void load()
    else setModels(null)
  }, [connected, load])

  const keyPresence = useMemo(() => {
    const map = new Map<string, boolean>()
    for (const key of keys) map.set(key.name, key.present)
    return map
  }, [keys])

  const healthOf = useCallback(
    (model: RoutingModel): HealthFilter => {
      const env = model.api_key_env
      if (!env) return 'ok'
      if (lockedKeys.includes(env)) return 'rate-limited'
      if (keyPresence.get(env) === false) return 'missing-key'
      return 'ok'
    },
    [lockedKeys, keyPresence],
  )

  const providers = useMemo(() => {
    const set = new Set((models ?? []).map((m) => providerOf(m.id)))
    return [...set].sort()
  }, [models])

  const keySlots = useMemo(() => {
    const set = new Set(
      (models ?? []).map((m) => m.api_key_env).filter((k): k is string => Boolean(k)),
    )
    return [...set].sort()
  }, [models])

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase()
    const list = (models ?? []).filter((model) => {
      if (needle && !model.id.toLowerCase().includes(needle)) return false
      if (provider !== 'all' && providerOf(model.id) !== provider) return false
      if (keyFilter !== 'all' && (model.api_key_env ?? '') !== keyFilter) return false
      if (status === 'enabled' && !model.enabled) return false
      if (status === 'disabled' && model.enabled) return false
      if (health !== 'all' && healthOf(model) !== health) return false
      return true
    })

    return [...list].sort((a, b) => {
      switch (sort) {
        case 'cost':
          return a.cost - b.cost || a.id.localeCompare(b.id)
        case 'id':
          return a.id.localeCompare(b.id)
        case 'provider':
          return (
            providerOf(a.id).localeCompare(providerOf(b.id)) || a.id.localeCompare(b.id)
          )
        default:
          return b.capability - a.capability || a.id.localeCompare(b.id)
      }
    })
  }, [models, search, provider, keyFilter, status, health, sort, healthOf])

  const toggle = async (model: RoutingModel) => {
    if (!bridge) return
    const key = modelKey(model)
    // The endpoint returns an empty 200, so there is nothing to read back —
    // update optimistically and reload only if the write failed.
    setModels((prev) =>
      prev ? prev.map((m) => (modelKey(m) === key ? { ...m, enabled: !m.enabled } : m)) : prev,
    )
    const result = await bridge.api.toggleModel(key)
    if (!result.ok) {
      setError(result.error ?? 'Toggle failed.')
      void load()
    }
  }

  /** Apply a state to everything currently filtered, skipping no-ops. */
  const bulk = async (enabled: boolean) => {
    if (!bridge) return
    const targets = filtered.filter((m) => m.enabled !== enabled)
    if (targets.length === 0) return
    setBusy(true)
    setModels((prev) => {
      if (!prev) return prev
      const ids = new Set(targets.map(modelKey))
      return prev.map((m) => (ids.has(modelKey(m)) ? { ...m, enabled } : m))
    })
    for (const model of targets) {
      const result = await bridge.api.toggleModel(modelKey(model))
      if (!result.ok) {
        setError(result.error ?? 'Bulk update failed partway through.')
        break
      }
    }
    setBusy(false)
    void load()
  }

  const resetFilters = () => {
    setSearch('')
    setProvider('all')
    setKeyFilter('all')
    setStatus('all')
    setHealth('all')
  }

  const filtersActive =
    search.trim() !== '' ||
    provider !== 'all' ||
    keyFilter !== 'all' ||
    status !== 'all' ||
    health !== 'all'

  if (!connected) {
    return (
      <EmptyState
        title="Not connected"
        description="The model catalog is served by the running orchestrator at /api/models. Connect to a forge instance to view or manage it."
      />
    )
  }

  if (models === null) {
    return (
      <div className="flex items-center gap-2 py-6 text-xs text-fg-3">
        <Spinner /> Loading catalog…
      </div>
    )
  }

  const enabledCount = models.filter((m) => m.enabled).length
  const lockedCount = models.filter((m) => healthOf(m) === 'rate-limited').length
  const missingCount = models.filter((m) => healthOf(m) === 'missing-key').length

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-[200px] flex-1">
          <Search
            size={12}
            strokeWidth={1.8}
            className="pointer-events-none absolute top-1/2 left-2 -translate-y-1/2 text-fg-4"
          />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Filter by model id"
            aria-label="Filter by model id"
            className="w-full pl-7"
          />
        </div>

        <FilterSelect label="Provider" value={provider} onChange={setProvider}>
          <option value="all">All providers</option>
          {providers.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </FilterSelect>

        <FilterSelect label="API key" value={keyFilter} onChange={setKeyFilter}>
          <option value="all">All keys</option>
          {keySlots.map((k) => (
            <option key={k} value={k}>
              {k}
              {lockedKeys.includes(k) ? ' (rate-limited)' : ''}
              {keyPresence.get(k) === false ? ' (not set)' : ''}
            </option>
          ))}
        </FilterSelect>

        <FilterSelect
          label="State"
          value={status}
          onChange={(v) => setStatus(v as StatusFilter)}
        >
          <option value="all">Any state</option>
          <option value="enabled">Enabled</option>
          <option value="disabled">Disabled</option>
        </FilterSelect>

        <FilterSelect
          label="Health"
          value={health}
          onChange={(v) => setHealth(v as HealthFilter)}
        >
          <option value="all">Any health</option>
          <option value="ok">Usable</option>
          <option value="rate-limited">Rate-limited</option>
          <option value="missing-key">Key not set</option>
        </FilterSelect>

        <FilterSelect label="Sort" value={sort} onChange={(v) => setSort(v as SortKey)}>
          <option value="capability">Capability</option>
          <option value="cost">Cost</option>
          <option value="id">Model id</option>
          <option value="provider">Provider</option>
        </FilterSelect>

        {filtersActive ? (
          <IconButton label="Clear filters" onClick={resetFilters}>
            <X size={13} strokeWidth={2} />
          </IconButton>
        ) : null}

        <IconButton label="Reload catalog" onClick={load} disabled={busy}>
          {busy ? <Spinner size={12} /> : <RefreshCw size={13} strokeWidth={1.8} />}
        </IconButton>
      </div>

      <div className="flex flex-wrap items-center gap-3 text-2xs text-fg-4">
        <span className="num">
          {filtered.length === models.length
            ? `${models.length} models`
            : `${filtered.length} of ${models.length} models`}
        </span>
        <span className="num">{enabledCount} enabled</span>
        {lockedCount > 0 ? (
          <button
            type="button"
            onClick={() => setHealth('rate-limited')}
            className="num text-st-waiting hover:underline"
          >
            {lockedCount} rate-limited
          </button>
        ) : null}
        {missingCount > 0 ? (
          <button
            type="button"
            onClick={() => setHealth('missing-key')}
            className="num text-st-failed hover:underline"
          >
            {missingCount} missing a key
          </button>
        ) : null}

        <span className="ml-auto flex items-center gap-1.5">
          <Button size="sm" disabled={busy || filtered.length === 0} onClick={() => bulk(true)}>
            Enable filtered
          </Button>
          <Button size="sm" disabled={busy || filtered.length === 0} onClick={() => bulk(false)}>
            Disable filtered
          </Button>
        </span>
      </div>

      {error ? (
        <div className="flex items-start gap-2 rounded-[var(--radius-card)] border border-st-failed/40 bg-st-failed-weak px-3 py-2 text-xs text-st-failed">
          <AlertTriangle size={13} strokeWidth={1.9} className="mt-0.5 shrink-0" />
          <span className="pretty min-w-0 flex-1">{error}</span>
        </div>
      ) : null}

      {filtered.length === 0 ? (
        <EmptyState
          className="py-10"
          title="No models match these filters"
          action={
            filtersActive ? (
              <Button size="sm" onClick={resetFilters}>
                Clear filters
              </Button>
            ) : undefined
          }
        />
      ) : (
        <div className="overflow-hidden rounded-[var(--radius-card)] border border-line-2">
          <div className="flex items-center gap-3 border-b border-line-1 bg-bg-2 px-3 py-1.5 text-2xs tracking-wide text-fg-4 uppercase">
            <span className="min-w-0 flex-1">Model</span>
            <span className="w-40 shrink-0">Key</span>
            <span className="w-16 shrink-0 text-right">Cost</span>
            <span className="w-16 shrink-0 text-right">Cap.</span>
            <span className="w-8 shrink-0" />
          </div>

          {filtered.map((model, index) => {
            const state = healthOf(model)
            return (
              <div
                key={`${modelKey(model)}-${index}`}
                className={cn(
                  'flex items-center gap-3 px-3 py-1.5',
                  index > 0 && 'border-t border-line-1',
                  model.enabled ? 'bg-bg-1' : 'bg-bg-0',
                )}
              >
                <span className="min-w-0 flex-1">
                  <span
                    className={cn(
                      'mono truncate-1 block text-xs',
                      model.enabled ? 'text-fg-1' : 'text-fg-3',
                    )}
                    title={model.id}
                  >
                    {model.id}
                  </span>
                </span>

                <span className="flex w-40 shrink-0 items-center gap-1">
                  {model.api_key_env ? (
                    <>
                      <KeyRound
                        size={9}
                        strokeWidth={2}
                        className={
                          state === 'ok'
                            ? 'shrink-0 text-fg-4'
                            : state === 'rate-limited'
                              ? 'shrink-0 text-st-waiting'
                              : 'shrink-0 text-st-failed'
                        }
                      />
                      <button
                        type="button"
                        onClick={() => setKeyFilter(model.api_key_env!)}
                        title={
                          state === 'rate-limited'
                            ? `${model.api_key_env} is rate-limited`
                            : state === 'missing-key'
                              ? `${model.api_key_env} is not set in .env`
                              : `Filter by ${model.api_key_env}`
                        }
                        className={cn(
                          'mono truncate-1 min-w-0 text-2xs hover:underline',
                          state === 'ok'
                            ? 'text-fg-3'
                            : state === 'rate-limited'
                              ? 'text-st-waiting'
                              : 'text-st-failed',
                        )}
                      >
                        {model.api_key_env.replace(/_API_KEY/, '')}
                      </button>
                    </>
                  ) : (
                    <span className="text-2xs text-fg-4">—</span>
                  )}
                </span>

                <span className="num w-16 shrink-0 text-right text-2xs text-fg-3">
                  {model.cost === 0 ? 'free' : model.cost.toFixed(2)}
                </span>
                <span className="num w-16 shrink-0 text-right text-2xs text-fg-3">
                  {model.capability.toFixed(1)}
                </span>

                <span className="flex w-8 shrink-0 justify-end">
                  <Toggle
                    label={`Enable ${model.id}`}
                    checked={model.enabled}
                    onChange={() => toggle(model)}
                  />
                </span>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

function FilterSelect({
  label,
  value,
  onChange,
  children,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  children: React.ReactNode
}) {
  return (
    <Select
      aria-label={label}
      value={value}
      onChange={(event) => onChange(event.target.value)}
      className="w-auto min-w-[132px]"
    >
      {children}
    </Select>
  )
}
