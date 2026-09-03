import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  AlertTriangle,
  Check,
  Eye,
  EyeOff,
  KeyRound,
  Pencil,
  Plus,
  RefreshCw,
  Trash2,
  X,
} from 'lucide-react'
import { cn } from '@/design/cn'
import { Button, IconButton, Input, Spinner } from '@/design/primitives'
import { bridge } from '@/state/bridge'
import { useStudio } from '@/state/store'
import type { EnvKeyEntry, KeyProvider } from '@shared/ipc'
import type { RoutingModel } from '@shared/events'

const PROVIDER_LABEL: Record<KeyProvider, string> = {
  openrouter: 'OpenRouter',
  groq: 'Groq',
  gemini: 'Gemini',
  other: 'Other variables',
}

const PROVIDER_ORDER: KeyProvider[] = ['openrouter', 'groq', 'gemini', 'other']

/**
 * API key management.
 *
 * Keys live in the repo-root `.env`, which `cmd/forge/main.go:23` parses at
 * startup. Two facts drive the whole design of this panel and are stated in it
 * rather than left for the user to discover:
 *
 *  - the router only reads a fixed set of variable names, so a key stored under
 *    any other name is silently ignored;
 *  - `.env` is read once at boot, so edits need a forge restart to take effect.
 */
export function KeysSection() {
  const [keys, setKeys] = useState<EnvKeyEntry[] | null>(null)
  const [models, setModels] = useState<RoutingModel[]>([])
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [path, setPath] = useState<string | null>(null)
  const [editing, setEditing] = useState<string | null>(null)
  const [addingCustom, setAddingCustom] = useState(false)

  const connected = useStudio((s) => s.connection.phase === 'connected')
  const lockedKeys = useStudio((s) => s.projection.waitlist?.lockedKeys ?? [])
  const forgePhase = useStudio((s) => s.forge.phase)

  const load = useCallback(async () => {
    if (!bridge) return
    setBusy(true)
    const [result, envPath] = await Promise.all([bridge.keys.list(), bridge.keys.path()])
    setBusy(false)
    setPath(envPath)
    if (result.ok && result.data) {
      setKeys(result.data)
      setError(null)
    } else {
      setError(result.error ?? 'Could not read .env')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  // Model counts per key slot come from the live router, so they only appear
  // while connected.
  useEffect(() => {
    if (!bridge || !connected) {
      setModels([])
      return
    }
    void bridge.api.models().then((r) => setModels(r.ok && r.data ? r.data : []))
  }, [connected])

  const modelsPerKey = useMemo(() => {
    const counts = new Map<string, number>()
    for (const model of models) {
      if (!model.api_key_env) continue
      counts.set(model.api_key_env, (counts.get(model.api_key_env) ?? 0) + 1)
    }
    return counts
  }, [models])

  const grouped = useMemo(() => {
    const map = new Map<KeyProvider, EnvKeyEntry[]>()
    for (const entry of keys ?? []) {
      const list = map.get(entry.provider) ?? []
      list.push(entry)
      map.set(entry.provider, list)
    }
    return map
  }, [keys])

  const write = async (fn: () => Promise<{ ok: boolean; error?: string }>) => {
    setBusy(true)
    const result = await fn()
    setBusy(false)
    if (!result.ok) {
      setError(result.error ?? 'Write failed.')
      return false
    }
    setError(null)
    setEditing(null)
    setAddingCustom(false)
    await load()
    return true
  }

  if (keys === null) {
    return (
      <div className="flex items-center gap-2 py-6 text-xs text-fg-3">
        <Spinner /> Reading .env…
      </div>
    )
  }

  const configured = keys.filter((k) => k.present).length
  const unknownPresent = keys.some((k) => !k.known && k.present)

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start gap-3 rounded-[var(--radius-card)] border border-line-2 bg-bg-1 px-3 py-2.5">
        <KeyRound size={14} strokeWidth={1.8} className="mt-0.5 shrink-0 text-fg-3" />
        <div className="min-w-0 flex-1">
          <p className="pretty text-xs text-fg-2">
            Keys are stored in{' '}
            <span className="mono text-fg-1">{path ?? '.env'}</span> and read once when
            forge starts.
            {forgePhase === 'running' ? (
              <span className="text-st-waiting">
                {' '}
                forge is running — restart it for changes to take effect.
              </span>
            ) : null}
          </p>
          <p className="num mt-1 text-2xs text-fg-4">
            {configured} of {keys.filter((k) => k.known).length} recognised slots
            configured
            {lockedKeys.length > 0 ? ` · ${lockedKeys.length} currently rate-limited` : ''}
          </p>
        </div>
        <IconButton label="Reload from disk" onClick={load} disabled={busy}>
          {busy ? <Spinner size={12} /> : <RefreshCw size={13} strokeWidth={1.8} />}
        </IconButton>
      </div>

      {error ? (
        <div className="flex items-start gap-2 rounded-[var(--radius-card)] border border-st-failed/40 bg-st-failed-weak px-3 py-2 text-xs text-st-failed">
          <AlertTriangle size={13} strokeWidth={1.9} className="mt-0.5 shrink-0" />
          <span className="pretty min-w-0 flex-1">{error}</span>
        </div>
      ) : null}

      {PROVIDER_ORDER.map((provider) => {
        const entries = grouped.get(provider)
        if (!entries || entries.length === 0) return null
        return (
          <section key={provider}>
            <div className="mb-1.5 flex items-baseline gap-2">
              <h2 className="text-xs font-semibold tracking-wide text-fg-2 uppercase">
                {PROVIDER_LABEL[provider]}
              </h2>
              {provider === 'other' ? (
                <span className="pretty text-2xs text-fg-4">
                  present in .env but never read by the router
                </span>
              ) : (
                <span className="num text-2xs text-fg-4">
                  {entries.filter((e) => e.present).length}/{entries.length} set
                </span>
              )}
            </div>

            <div className="overflow-hidden rounded-[var(--radius-card)] border border-line-2">
              {entries.map((entry, index) => (
                <KeyRow
                  key={entry.name}
                  entry={entry}
                  first={index === 0}
                  locked={lockedKeys.includes(entry.name)}
                  modelCount={modelsPerKey.get(entry.name)}
                  editing={editing === entry.name}
                  busy={busy}
                  onEdit={() => setEditing(entry.name)}
                  onCancel={() => setEditing(null)}
                  onSave={(value) => write(() => bridge!.keys.set(entry.name, value))}
                  onRemove={() => write(() => bridge!.keys.remove(entry.name))}
                />
              ))}
            </div>
          </section>
        )
      })}

      {unknownPresent ? (
        <p className="pretty flex items-start gap-2 text-2xs text-st-waiting">
          <AlertTriangle size={12} strokeWidth={1.9} className="mt-0.5 shrink-0" />
          Variables outside the recognised slots are kept in the file untouched, but the
          router will never read them — it scans a hardcoded list of names.
        </p>
      ) : null}

      {addingCustom ? (
        <CustomVarForm
          busy={busy}
          onCancel={() => setAddingCustom(false)}
          onSave={(name, value) => write(() => bridge!.keys.set(name, value))}
        />
      ) : (
        <div>
          <Button
            size="sm"
            icon={<Plus size={12} strokeWidth={2} />}
            onClick={() => setAddingCustom(true)}
          >
            Add variable
          </Button>
        </div>
      )}
    </div>
  )
}

function KeyRow({
  entry,
  first,
  locked,
  modelCount,
  editing,
  busy,
  onEdit,
  onCancel,
  onSave,
  onRemove,
}: {
  entry: EnvKeyEntry
  first: boolean
  locked: boolean
  modelCount?: number
  editing: boolean
  busy: boolean
  onEdit: () => void
  onCancel: () => void
  onSave: (value: string) => void
  onRemove: () => void
}) {
  const [draft, setDraft] = useState('')
  const [revealed, setRevealed] = useState<string | null>(null)

  useEffect(() => {
    if (!editing) setDraft('')
  }, [editing])

  // A revealed secret hides itself again rather than sitting on screen.
  useEffect(() => {
    if (revealed === null) return
    const id = setTimeout(() => setRevealed(null), 20_000)
    return () => clearTimeout(id)
  }, [revealed])

  const reveal = async () => {
    if (revealed !== null) {
      setRevealed(null)
      return
    }
    const result = await bridge?.keys.reveal(entry.name)
    if (result?.ok) setRevealed(result.data ?? '')
  }

  return (
    <div
      className={cn(
        'flex items-center gap-3 px-3 py-2',
        !first && 'border-t border-line-1',
        entry.present ? 'bg-bg-1' : 'bg-bg-0',
      )}
    >
      <span
        className="h-1.5 w-1.5 shrink-0 rounded-full"
        style={{
          backgroundColor: locked
            ? 'var(--color-st-waiting)'
            : entry.present
              ? 'var(--color-st-done)'
              : 'var(--color-st-idle)',
        }}
        title={locked ? 'Rate-limited' : entry.present ? 'Set' : 'Not set'}
      />

      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="mono truncate-1 text-xs text-fg-1">{entry.name}</span>
          {entry.slotLabel ? (
            <span className="text-2xs text-fg-4">{entry.slotLabel}</span>
          ) : null}
          {locked ? (
            <span className="rounded-[3px] bg-st-waiting-weak px-1 text-2xs text-st-waiting">
              rate-limited
            </span>
          ) : null}
          {modelCount !== undefined && modelCount > 0 ? (
            <span className="num text-2xs text-fg-4">{modelCount} models</span>
          ) : null}
        </div>

        {editing ? (
          <div className="mt-1.5 flex items-center gap-1">
            <Input
              autoFocus
              type="password"
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && draft.trim()) onSave(draft)
                if (event.key === 'Escape') onCancel()
              }}
              placeholder={`Paste the ${entry.name} value`}
              className="mono h-7 w-full flex-1 text-2xs"
            />
            <IconButton
              label="Save"
              size="sm"
              disabled={busy || !draft.trim()}
              onClick={() => onSave(draft)}
            >
              <Check size={13} strokeWidth={2.2} />
            </IconButton>
            <IconButton label="Cancel" size="sm" onClick={onCancel}>
              <X size={13} strokeWidth={2} />
            </IconButton>
          </div>
        ) : (
          <div className="mono mt-0.5 truncate-1 text-2xs text-fg-3">
            {entry.present ? (
              (revealed ?? entry.masked)
            ) : (
              <span className="text-fg-4 italic">not set</span>
            )}
            {entry.present && revealed !== null ? (
              <span className="ml-2 text-fg-4 not-italic">hides in 20s</span>
            ) : null}
          </div>
        )}
      </div>

      {!editing ? (
        <div className="flex shrink-0 items-center gap-0.5">
          {entry.present ? (
            <IconButton
              label={revealed !== null ? 'Hide value' : 'Reveal value'}
              size="sm"
              onClick={reveal}
            >
              {revealed !== null ? (
                <EyeOff size={13} strokeWidth={1.8} />
              ) : (
                <Eye size={13} strokeWidth={1.8} />
              )}
            </IconButton>
          ) : null}
          <IconButton
            label={entry.present ? 'Replace value' : 'Add key'}
            size="sm"
            onClick={onEdit}
          >
            {entry.present ? (
              <Pencil size={12} strokeWidth={1.9} />
            ) : (
              <Plus size={13} strokeWidth={2} />
            )}
          </IconButton>
          {entry.present ? (
            <IconButton
              label={`Remove ${entry.name} from .env`}
              size="sm"
              disabled={busy}
              onClick={onRemove}
              className="hover:text-st-failed"
            >
              <Trash2 size={12} strokeWidth={1.9} />
            </IconButton>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

function CustomVarForm({
  busy,
  onCancel,
  onSave,
}: {
  busy: boolean
  onCancel: () => void
  onSave: (name: string, value: string) => void
}) {
  const [name, setName] = useState('')
  const [value, setValue] = useState('')
  const valid = /^[A-Za-z_][A-Za-z0-9_]*$/.test(name)

  return (
    <div className="rounded-[var(--radius-card)] border border-line-2 bg-bg-1 p-3">
      <div className="mb-2 flex items-center gap-2">
        <Input
          autoFocus
          value={name}
          onChange={(event) => setName(event.target.value.toUpperCase())}
          placeholder="VARIABLE_NAME"
          className="mono h-7 w-64 shrink-0 text-2xs"
        />
        <Input
          type="password"
          value={value}
          onChange={(event) => setValue(event.target.value)}
          placeholder="value"
          className="mono h-7 w-full flex-1 text-2xs"
        />
      </div>
      <div className="flex items-center gap-2">
        <p className="pretty min-w-0 flex-1 text-2xs text-fg-4">
          {name && !valid
            ? 'Names may contain only letters, digits and underscores.'
            : 'Only the recognised slot names above are read by the router; anything else is stored but unused.'}
        </p>
        <Button size="sm" onClick={onCancel}>
          Cancel
        </Button>
        <Button
          size="sm"
          variant="primary"
          disabled={busy || !valid || !value.trim()}
          onClick={() => onSave(name, value)}
        >
          Save
        </Button>
      </div>
    </div>
  )
}
