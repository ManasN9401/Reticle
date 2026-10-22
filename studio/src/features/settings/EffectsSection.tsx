import { useCallback, useEffect, useRef, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { Button, EmptyState, Input, Select, Spinner } from '@/design/primitives'
import { bridge } from '@/state/bridge'
import { useStudio } from '@/state/store'
import type { EffectReconcile, EffectSnapshot, ExternalEffect } from '@shared/ipc'

export function EffectsSection() {
  const connected = useStudio((state) => state.connection.phase === 'connected')
  return connected ? <ConnectedEffectsSection /> : <EmptyState title="Not connected" description="Connect to Forge to inspect external operations." />
}

function ConnectedEffectsSection() {
  const [snapshot, setSnapshot] = useState<EffectSnapshot | null>(null)
  const [selected, setSelected] = useState<ExternalEffect | null>(null)
  const [outcome, setOutcome] = useState<EffectReconcile['state']>('failed')
  const [observation, setObservation] = useState('')
  const [externalId, setExternalId] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showAll, setShowAll] = useState(false)
  const generation = useRef(0)
  const invalidateLoads = useCallback(() => { generation.current++ }, [])

  const load = useCallback(async () => {
    if (!bridge) return
    const request = ++generation.current
    try {
      const result = await bridge.api.effects(showAll ? undefined : 'interrupted')
      if (request !== generation.current) return
      if (result.ok && result.data) { setSnapshot(result.data); setError(null) }
      else setError(result.error ?? 'Could not load external effects.')
    } catch (cause) { if (request === generation.current) setError(String(cause)) }
  }, [showAll])

  useEffect(() => {
    let cancelled = false
    queueMicrotask(() => { if (!cancelled) void load() })
    return () => { cancelled = true; invalidateLoads() }
  }, [load, invalidateLoads])

  const reconcile = async () => {
    if (!bridge || !selected || !observation.trim() || busy) return
    setBusy(true)
    try {
      const result = await bridge.api.reconcileEffect({ id: selected.id, state: outcome,
        observation: observation.trim(), external_id: externalId.trim() || undefined })
      if (!result.ok) { setError(result.error ?? 'Reconciliation failed.'); return }
      setSelected(null); setObservation(''); setExternalId('')
      await load()
    } catch (cause) { setError(String(cause)) }
    finally { setBusy(false) }
  }

  if (snapshot === null && !error) return <div className="flex items-center gap-2 text-xs text-fg-3"><Spinner /> Loading external effects…</div>
  const records = snapshot?.effects.filter((record) => showAll || record.state === 'interrupted') ?? []
  return <div className="flex min-w-0 flex-col gap-4">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <p className="max-w-[70ch] text-xs text-fg-3">Check the operation in the external service, then record what happened. Resolving a record updates its recovery status.</p>
      <div className="flex gap-2"><Button size="sm" disabled={busy} onClick={() => setShowAll(!showAll)}>{showAll ? 'Show interrupted' : 'Show all'}</Button><Button size="sm" icon={<RefreshCw size={13} />} onClick={load} disabled={busy}>Refresh</Button></div>
    </div>
    {error ? <p role="alert" className="rounded border border-st-failed/40 bg-st-failed-weak p-3 text-xs text-st-failed">{error}</p> : null}
    {snapshot?.truncated ? <p className="text-xs text-fg-3">Showing the latest 1,000 operations. Older records remain in the runtime ledger.</p> : null}
    {snapshot !== null && records.length === 0 ? <EmptyState title={showAll ? 'No external operations' : 'No interrupted operations'} description="Refresh after a tool operation or interrupted run." /> : null}
    {records.map((record) => <article key={record.id} className="min-w-0 rounded-[var(--radius-card)] border border-line-2 bg-bg-1 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2"><strong className="break-all text-xs text-fg-1">{record.target}</strong><span className="text-xs text-fg-3">{record.state}</span></div>
      <p className="mt-1 break-all text-xs text-fg-3">{record.task_id ?? record.execution ?? record.attempt_id}</p>
      <p className="mt-1 break-all text-xs text-fg-3">{record.detail}</p>
      <details className="mt-2 text-xs text-fg-3"><summary className="cursor-pointer">Operation details</summary><dl className="mt-2 grid gap-1 break-all"><dt>Operation</dt><dd>{record.id}</dd><dt>Adapter</dt><dd>{record.adapter}</dd><dt>Request digest</dt><dd>{record.request_hash}</dd><dt>Updated</dt><dd>{record.updated_at}</dd>{record.external_id ? <><dt>External reference</dt><dd>{record.external_id}</dd></> : null}</dl></details>
      {record.state === 'interrupted' ? <Button size="sm" className="mt-3" disabled={busy} onClick={() => { setSelected(record); setObservation(''); setExternalId(record.external_id ?? ''); setOutcome('failed') }}>Record observed outcome</Button> : null}
      {record.state === 'interrupted' && selected?.id === record.id ? <form className="mt-3 grid gap-3" onSubmit={(event) => { event.preventDefault(); void reconcile() }}>
        <label className="grid gap-1 text-xs text-fg-2">Observed outcome<Select value={outcome} disabled={busy} onChange={(event) => setOutcome(event.target.value as EffectReconcile['state'])}><option value="failed">Failed or confirmed absent</option><option value="succeeded">Succeeded</option><option value="cancelled">Cancelled</option></Select></label>
        <label className="grid gap-1 text-xs text-fg-2">External reference (optional)<Input value={externalId} disabled={busy} maxLength={1024} onChange={(event) => setExternalId(event.target.value)} /></label>
        <label className="grid gap-1 text-xs text-fg-2">What did you observe?<textarea required maxLength={4096} rows={3} disabled={busy} value={observation} onChange={(event) => setObservation(event.target.value)} placeholder="What did you check, and what did the service report?" className="w-full rounded border border-line-2 bg-bg-0 p-2 text-fg-1" /></label>
        <div className="flex gap-2"><Button variant="primary" type="submit" disabled={busy || !observation.trim()}>{busy ? 'Saving…' : 'Save observed outcome'}</Button><Button type="button" disabled={busy} onClick={() => setSelected(null)}>Cancel</Button></div>
      </form> : null}
    </article>)}
  </div>
}
