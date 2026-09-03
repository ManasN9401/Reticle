import { useEffect, useState, type ReactNode } from 'react'
import { FolderOpen, RefreshCw, Search } from 'lucide-react'
import { cn } from '@/design/cn'
import {
  Button,
  Divider,
  EmptyState,
  IconButton,
  Input,
  Select,
  Spinner,
  Toggle,
} from '@/design/primitives'
import { bridge } from '@/state/bridge'
import { useStudio } from '@/state/store'
import { modelKey, type RoutingModel } from '@shared/events'
import type { SettingsPatch } from '@shared/ipc'
import { SETTINGS_SECTIONS, useSettingsUi } from './state'

/**
 * Settings.
 *
 * Section-driven so the surface can grow — Studio preferences, forge.exe flags
 * and LLM routing all belong here eventually — without any section needing to
 * know about the others.
 */
export function SettingsView() {
  const section = useSettingsUi((s) => s.section)
  const settings = useStudio((s) => s.settings)

  const patch = async (next: SettingsPatch) => {
    await bridge?.settings.patch(next)
  }

  if (!settings) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-xs text-fg-3">
        <Spinner /> Loading settings…
      </div>
    )
  }

  const meta = SETTINGS_SECTIONS.find((s) => s.id === section)

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-bg-0">
      <header className="shrink-0 border-b border-line-1 px-6 py-4">
        <h1 className="balance text-lg font-medium text-fg-1">{meta?.label}</h1>
        <p className="pretty mt-0.5 text-xs text-fg-3">{meta?.hint}</p>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
        <div className="max-w-[640px]">
          {section === 'appearance' ? (
            <Group>
              <SettingRow
                label="Density"
                description="Compact tightens row heights and tab strips for dense sessions."
              >
                <Select
                  value={settings.appearance.density}
                  onChange={(event) =>
                    patch({
                      appearance: {
                        density: event.target.value as 'comfortable' | 'compact',
                      },
                    })
                  }
                  className="w-40"
                >
                  <option value="comfortable">Comfortable</option>
                  <option value="compact">Compact</option>
                </Select>
              </SettingRow>

              <SettingRow
                label="Reduce motion"
                description="Disables the running-node sweep and edge flow. Your OS setting is always honoured regardless."
              >
                <Toggle
                  label="Reduce motion"
                  checked={settings.appearance.reduceMotion}
                  onChange={(reduceMotion) => patch({ appearance: { reduceMotion } })}
                />
              </SettingRow>
            </Group>
          ) : null}

          {section === 'connection' ? (
            <Group>
              <SettingRow label="Host" description="Where the telemetry server is listening.">
                <Input
                  className="w-56"
                  value={settings.connection.host}
                  onChange={(event) =>
                    patch({ connection: { host: event.target.value } })
                  }
                />
              </SettingRow>
              <SettingRow
                label="Port"
                description="Must match forge's -port flag. Default 8080."
              >
                <Input
                  className="num w-28"
                  type="number"
                  value={settings.connection.port}
                  onChange={(event) =>
                    patch({ connection: { port: Number(event.target.value) || 8080 } })
                  }
                />
              </SettingRow>
              <SettingRow
                label="Connect on launch"
                description="Attach to the orchestrator automatically when Studio starts."
              >
                <Toggle
                  label="Auto connect"
                  checked={settings.connection.autoConnect}
                  onChange={(autoConnect) => patch({ connection: { autoConnect } })}
                />
              </SettingRow>
            </Group>
          ) : null}

          {section === 'forge' ? (
            <Group>
              <SettingRow
                label="Binary"
                description="Build it with: cd cmd/forge && go build -o forge.exe"
              >
                <div className="flex w-full max-w-[380px] items-center gap-1">
                  <Input
                    className="mono flex-1 text-2xs"
                    value={settings.forge.binaryPath}
                    onChange={(event) =>
                      patch({ forge: { binaryPath: event.target.value } })
                    }
                  />
                  <IconButton
                    label="Browse for the forge binary"
                    onClick={async () => {
                      const files = await bridge?.workspace.pickFiles()
                      if (files && files[0]) patch({ forge: { binaryPath: files[0] } })
                    }}
                  >
                    <FolderOpen size={14} strokeWidth={1.7} />
                  </IconButton>
                </div>
              </SettingRow>

              <SettingRow
                label="Working directory"
                description="Must be <repo>/cmd/forge. The runtime resolves its root as ../../ from here, so anywhere else silently points logs, sessions and artifacts at the wrong place."
              >
                <div className="flex w-full max-w-[380px] items-center gap-1">
                  <Input
                    className="mono flex-1 text-2xs"
                    value={settings.forge.cwd}
                    onChange={(event) => patch({ forge: { cwd: event.target.value } })}
                  />
                  <IconButton
                    label="Browse for the working directory"
                    onClick={async () => {
                      const dir = await bridge?.workspace.pickDirectory()
                      if (dir) patch({ forge: { cwd: dir } })
                    }}
                  >
                    <FolderOpen size={14} strokeWidth={1.7} />
                  </IconButton>
                </div>
              </SettingRow>

              <SettingRow
                label="Batch size"
                description="Concurrent workflows (-batch). Caps how many queue items run at once."
              >
                <Input
                  className="num w-28"
                  type="number"
                  min={1}
                  value={settings.forge.batch}
                  onChange={(event) =>
                    patch({ forge: { batch: Number(event.target.value) || 1 } })
                  }
                />
              </SettingRow>

              <SettingRow
                label="Retries"
                description="Per-node retry budget (-retries) for rate limits and transient failures."
              >
                <Input
                  className="num w-28"
                  type="number"
                  min={0}
                  value={settings.forge.retries}
                  onChange={(event) =>
                    patch({ forge: { retries: Number(event.target.value) || 0 } })
                  }
                />
              </SettingRow>

              <SettingRow
                label="Isolated sessions"
                description="Compile a fresh workspace per prompt under .reticle/sessions (-isolated)."
              >
                <Toggle
                  label="Isolated sessions"
                  checked={settings.forge.isolated}
                  onChange={(isolated) => patch({ forge: { isolated } })}
                />
              </SettingRow>

              <SettingRow
                label="Load all models"
                description="Load the full model catalog instead of only the premium tier (-all-models)."
              >
                <Toggle
                  label="All models"
                  checked={settings.forge.allModels}
                  onChange={(allModels) => patch({ forge: { allModels } })}
                />
              </SettingRow>

              <p className="pretty mt-2 text-2xs text-fg-4">
                Studio always passes <span className="mono text-fg-3">-native</span>.
                Without it, forge probes Docker and, on failure, blocks on an interactive
                stdin prompt that a spawned process can never answer.
              </p>
            </Group>
          ) : null}

          {section === 'models' ? <ModelsSection /> : null}

          {section === 'logs' ? (
            <Group>
              <SettingRow
                label="Buffer size"
                description="Records held in the main process. WorkerLog is roughly 77% of all events, so this fills quickly on a busy run."
              >
                <Input
                  className="num w-32"
                  type="number"
                  min={1000}
                  step={1000}
                  value={settings.logs.bufferSize}
                  onChange={(event) =>
                    patch({
                      logs: { bufferSize: Number(event.target.value) || 50_000 },
                    })
                  }
                />
              </SettingRow>
            </Group>
          ) : null}

          {section === 'about' ? <AboutSection /> : null}
        </div>
      </div>
    </div>
  )
}

function Group({ children }: { children: ReactNode }) {
  return <div className="flex flex-col">{children}</div>
}

function SettingRow({
  label,
  description,
  children,
}: {
  label: string
  description?: string
  children: ReactNode
}) {
  return (
    <div className="flex items-start gap-6 border-b border-line-1 py-3.5">
      <div className="min-w-0 flex-1">
        <div className="text-sm text-fg-1">{label}</div>
        {description ? (
          <p className="pretty mt-0.5 text-2xs leading-relaxed text-fg-4">
            {description}
          </p>
        ) : null}
      </div>
      <div className="flex shrink-0 items-center justify-end">{children}</div>
    </div>
  )
}

function ModelsSection() {
  const connected = useStudio((s) => s.connection.phase === 'connected')
  const [models, setModels] = useState<RoutingModel[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [search, setSearch] = useState('')

  const load = async () => {
    if (!bridge) return
    setBusy(true)
    const result = await bridge.api.models()
    setBusy(false)
    if (result.ok && result.data) {
      setModels(result.data)
      setError(null)
    } else {
      setError(result.error ?? 'Could not read the model catalog.')
    }
  }

  useEffect(() => {
    if (connected) void load()
    else setModels(null)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected])

  const toggle = async (model: RoutingModel) => {
    if (!bridge) return
    const key = modelKey(model)
    // Optimistic: the endpoint returns an empty 200, so there is nothing to
    // read back. Reload afterwards to confirm.
    setModels((prev) =>
      prev
        ? prev.map((m) => (modelKey(m) === key ? { ...m, enabled: !m.enabled } : m))
        : prev,
    )
    const result = await bridge.api.toggleModel(key)
    if (!result.ok) {
      setError(result.error ?? 'Toggle failed.')
      void load()
    }
  }

  if (!connected) {
    return (
      <EmptyState
        title="Not connected"
        description="The model catalog is served by the running orchestrator at /api/models. Connect to a forge instance to manage it."
      />
    )
  }

  const filtered = (models ?? []).filter((model) =>
    model.id.toLowerCase().includes(search.trim().toLowerCase()),
  )

  return (
    <div>
      <div className="mb-3 flex items-center gap-2">
        <div className="relative flex-1">
          <Search
            size={12}
            strokeWidth={1.8}
            className="pointer-events-none absolute top-1/2 left-2 -translate-y-1/2 text-fg-4"
          />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Filter models"
            aria-label="Filter models"
            className="pl-7"
          />
        </div>
        <Button
          size="md"
          onClick={load}
          disabled={busy}
          icon={busy ? <Spinner size={11} /> : <RefreshCw size={12} strokeWidth={1.8} />}
        >
          Reload
        </Button>
      </div>

      {error ? <p className="mb-2 text-2xs text-st-failed">{error}</p> : null}

      {models === null ? (
        <div className="flex items-center gap-2 py-6 text-xs text-fg-3">
          <Spinner /> Loading catalog…
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState title="No models match" />
      ) : (
        <div className="overflow-hidden rounded-[var(--radius-card)] border border-line-2">
          {filtered.map((model, index) => (
            <div
              key={modelKey(model)}
              className={cn(
                'flex items-center gap-3 px-3 py-2',
                index > 0 && 'border-t border-line-1',
                model.enabled ? 'bg-bg-1' : 'bg-bg-0',
              )}
            >
              <span className="min-w-0 flex-1">
                <span className="mono truncate-1 block text-xs text-fg-1">{model.id}</span>
                {model.api_key_env ? (
                  <span className="mono block text-2xs text-fg-4">
                    {model.api_key_env}
                  </span>
                ) : null}
              </span>
              <span className="num w-20 shrink-0 text-right text-2xs text-fg-3" title="Cost">
                {model.cost.toFixed(2)}
              </span>
              <span
                className="num w-20 shrink-0 text-right text-2xs text-fg-3"
                title="Estimated capability"
              >
                {model.capability.toFixed(2)}
              </span>
              <Toggle
                label={`Enable ${model.id}`}
                checked={model.enabled}
                onChange={() => toggle(model)}
              />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function AboutSection() {
  const settings = useStudio((s) => s.settings)
  const connection = useStudio((s) => s.connection)
  const forge = useStudio((s) => s.forge)
  const projection = useStudio((s) => s.projection)

  return (
    <div className="flex flex-col">
      <AboutRow label="Studio" value={bridge?.version ?? 'unknown'} />
      <AboutRow label="Session" value={projection.sessionId ?? '—'} />
      <AboutRow label="Events seen" value={projection.eventCount.toLocaleString()} />
      <AboutRow
        label="Orchestrator"
        value={`${connection.host}:${connection.port} · ${connection.phase}`}
      />
      <Divider />
      <AboutRow label="Forge binary" value={settings?.forge.binaryPath || '—'} />
      <AboutRow label="Forge cwd" value={settings?.forge.cwd || '—'} />
      <AboutRow
        label="Last launch args"
        value={forge.args ? forge.args.join(' ') : '—'}
      />
    </div>
  )
}

function AboutRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-4 border-b border-line-1 py-2">
      <span className="w-36 shrink-0 text-xs text-fg-3">{label}</span>
      <span className="mono min-w-0 flex-1 text-2xs break-all text-fg-2">{value}</span>
    </div>
  )
}
