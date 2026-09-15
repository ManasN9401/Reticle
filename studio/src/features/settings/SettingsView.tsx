import type { ReactNode } from 'react'
import { FolderOpen, Play, Square } from 'lucide-react'
import { cn } from '@/design/cn'
import {
  Button,
  Divider,
  IconButton,
  Input,
  Select,
  Spinner,
  StatusPip,
  Toggle,
} from '@/design/primitives'
import { connectionVar, formatRelative } from '@/design/status'
import {
  canStartForge,
  canStopForge,
  connect,
  disconnect,
  stopForge,
} from '@/state/actions'
import { bridge } from '@/state/bridge'
import { useStudio } from '@/state/store'
import { useUi } from '@/state/ui'
import type { NodeStyle, SettingsPatch, ThemePreference } from '@shared/ipc'
import { KeysSection } from './KeysSection'
import { ModelsSection } from './ModelsSection'
import { SETTINGS_SECTIONS, useSettingsUi } from './state'

/**
 * Settings.
 *
 * Section-driven so the surface can keep growing without any section needing to
 * know about the others. Wide sections (Models, API Keys) get the full width;
 * form-shaped ones are held to a readable measure.
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
  const wide = section === 'models' || section === 'keys'

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-bg-0">
      <header className="shrink-0 border-b border-line-1 px-6 py-4">
        <h1 className="balance text-lg font-medium text-fg-1">{meta?.label}</h1>
        <p className="pretty mt-0.5 max-w-[70ch] text-xs text-fg-3">{meta?.description}</p>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
        <div className={wide ? 'max-w-[1100px]' : 'max-w-[640px]'}>
          {section === 'appearance' ? <AppearanceSection patch={patch} /> : null}
          {section === 'connection' ? <ConnectionSection patch={patch} /> : null}
          {section === 'forge' ? <ForgeSection patch={patch} /> : null}
          {section === 'keys' ? <KeysSection /> : null}
          {section === 'models' ? <ModelsSection /> : null}
          {section === 'logs' ? <LogsSection patch={patch} /> : null}
          {section === 'about' ? <AboutSection /> : null}
        </div>
      </div>
    </div>
  )
}

type Patch = (next: SettingsPatch) => Promise<void>

// ---------------------------------------------------------------------------

function AppearanceSection({ patch }: { patch: Patch }) {
  const appearance = useStudio((s) => s.settings!.appearance)
  return (
    <div className="flex flex-col">
      <Row
        label="Theme"
        description="Match System follows your OS colour scheme and switches live when it changes."
      >
        <Select
          value={appearance.theme}
          onChange={(event) =>
            patch({ appearance: { theme: event.target.value as ThemePreference } })
          }
          className="w-40"
        >
          <option value="system">Match System</option>
          <option value="dark">Dark</option>
          <option value="light">Light</option>
        </Select>
      </Row>

      <Row
        label="Node map style"
        description="Detailed shows model, artifacts and retries per node. Compact drops to one line for graphs of dozens. Hexagonal is densest and echoes the embedded telemetry star map."
      >
        <Select
          value={appearance.nodeStyle}
          onChange={(event) =>
            patch({ appearance: { nodeStyle: event.target.value as NodeStyle } })
          }
          className="w-40"
        >
          <option value="detailed">Detailed</option>
          <option value="compact">Compact</option>
          <option value="hex">Hexagonal</option>
        </Select>
      </Row>

      <Row
        label="Density"
        description="Compact tightens row heights, tab strips and toolbars for long sessions on a small screen."
      >
        <Select
          value={appearance.density}
          onChange={(event) =>
            patch({
              appearance: { density: event.target.value as 'comfortable' | 'compact' },
            })
          }
          className="w-40"
        >
          <option value="comfortable">Comfortable</option>
          <option value="compact">Compact</option>
        </Select>
      </Row>

      <Row
        label="Reduce motion"
        description="Disables the running-node sweep and the edge flow animation. Your operating system's reduced-motion setting is always honoured regardless of this."
      >
        <Toggle
          label="Reduce motion"
          checked={appearance.reduceMotion}
          onChange={(reduceMotion) => patch({ appearance: { reduceMotion } })}
        />
      </Row>
    </div>
  )
}

// ---------------------------------------------------------------------------

function ConnectionSection({ patch }: { patch: Patch }) {
  const connection = useStudio((s) => s.settings!.connection)
  const live = useStudio((s) => s.connection)
  const connected = live.phase === 'connected'

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3 rounded-[var(--radius-card)] border border-line-2 bg-bg-1 px-3 py-2.5">
        <StatusPip
          color={connectionVar(live.phase)}
          pulse={live.phase === 'connecting' || live.phase === 'reconnecting'}
        />
        <div className="min-w-0 flex-1">
          <div className="text-xs text-fg-1">
            {connected ? 'Connected' : live.phase === 'idle' ? 'Disconnected' : live.phase}
            <span className="mono ml-2 text-fg-4">
              {live.host}:{live.port}
            </span>
          </div>
          <div className="num mt-0.5 text-2xs text-fg-4">
            {connected
              ? `since ${formatRelative(live.connectedAt)} · ${live.eventRate} events/s`
              : live.attempt > 0
                ? `retry attempt ${live.attempt}${live.message ? ` · ${live.message}` : ''}`
                : 'not attached to an orchestrator'}
          </div>
        </div>
        <Button size="sm" onClick={() => (connected ? disconnect() : connect())}>
          {connected ? 'Disconnect' : 'Connect'}
        </Button>
      </div>

      <div className="flex flex-col">
        <Row label="Host" description="Where the telemetry server is listening.">
          <Input
            className="w-56"
            value={connection.host}
            onChange={(event) => patch({ connection: { host: event.target.value } })}
          />
        </Row>
        <Row
          label="Port"
          description="Must match the -port forge was started with. Default 8080. A mismatch shows up as an endless Reconnecting state."
        >
          <Input
            className="num w-28"
            type="number"
            value={connection.port}
            onChange={(event) =>
              patch({ connection: { port: Number(event.target.value) || 8080 } })
            }
          />
        </Row>
        <Row
          label="Connect on launch"
          description="Attach to the orchestrator automatically when Studio starts."
        >
          <Toggle
            label="Auto connect"
            checked={connection.autoConnect}
            onChange={(autoConnect) => patch({ connection: { autoConnect } })}
          />
        </Row>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------

function ForgeSection({ patch }: { patch: Patch }) {
  const forgeSettings = useStudio((s) => s.settings!.forge)
  const forge = useStudio((s) => s.forge)

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3 rounded-[var(--radius-card)] border border-line-2 bg-bg-1 px-3 py-2.5">
        <StatusPip
          color={
            forge.phase === 'running'
              ? 'var(--color-st-running)'
              : forge.phase === 'error'
                ? 'var(--color-st-failed)'
                : 'var(--color-st-idle)'
          }
          pulse={forge.phase === 'running'}
        />
        <div className="min-w-0 flex-1">
          <div className="text-xs text-fg-1">
            forge is {forge.phase}
            {forge.pid !== undefined ? (
              <span className="num mono ml-2 text-fg-4">pid {forge.pid}</span>
            ) : null}
            {forge.exitCode !== undefined && forge.exitCode !== null ? (
              <span className="num mono ml-2 text-fg-4">exit {forge.exitCode}</span>
            ) : null}
          </div>
          <div className="mono mt-0.5 truncate-1 text-2xs text-fg-4">
            {forge.args ? forge.args.join(' ') : 'not launched from Studio this session'}
          </div>
          {forge.message ? (
            <div className="pretty mt-1 text-2xs text-st-failed">{forge.message}</div>
          ) : null}
        </div>
        <Button
          size="sm"
          disabled={!canStartForge() && !canStopForge()}
          icon={
            canStopForge() ? (
              <Square size={11} strokeWidth={2.2} />
            ) : (
              <Play size={11} strokeWidth={2.2} />
            )
          }
          onClick={() => (canStopForge() ? stopForge() : useUi.getState().setLauncherOpen(true))}
        >
          {canStopForge() ? 'Stop' : 'Start'}
        </Button>
      </div>

      <div className="flex flex-col">
        <Row
          label="Binary"
          description="Build it with: cd cmd/forge && go build -o forge.exe"
        >
          <PathInput
            value={forgeSettings.binaryPath}
            onChange={(binaryPath) => patch({ forge: { binaryPath } })}
            pick={() => bridge?.workspace.pickFiles().then((f) => f[0] ?? null)}
          />
        </Row>

        <Row
          label="Working directory"
          description="Must be <repo>/cmd/forge. The runtime resolves its root as ../../ from here, so anywhere else silently points logs, sessions and artifacts at the wrong place."
        >
          <PathInput
            value={forgeSettings.cwd}
            onChange={(cwd) => patch({ forge: { cwd } })}
            pick={() => bridge?.workspace.pickDirectory() ?? Promise.resolve(null)}
          />
        </Row>

        <Row
          label="Workspace"
          description="Existing workspace to resume from (-workspace). Clear this to start a fresh workspace."
        >
          <PathInput
            value={forgeSettings.workspace ?? ''}
            onChange={(workspace) => patch({ forge: { workspace: workspace || undefined } })}
            pick={() => bridge?.workspace.pickDirectory() ?? Promise.resolve(null)}
          />
        </Row>

        <Row
          label="Batch size"
          description="Concurrent workflows (-batch). Caps how many queued items run at once."
        >
          <Input
            className="num w-28"
            type="number"
            min={1}
            value={forgeSettings.batch}
            onChange={(event) =>
              patch({ forge: { batch: Number(event.target.value) || 1 } })
            }
          />
        </Row>

        <Row
          label="Retries"
          description="Per-node retry budget (-retries) for rate limits and transient failures."
        >
          <Input
            className="num w-28"
            type="number"
            min={0}
            value={forgeSettings.retries}
            onChange={(event) =>
              patch({ forge: { retries: Number(event.target.value) || 0 } })
            }
          />
        </Row>

        <Row
          label="Isolated sessions"
          description="Compile a fresh workspace per prompt under .reticle/sessions (-isolated)."
        >
          <Toggle
            label="Isolated sessions"
            checked={forgeSettings.isolated}
            onChange={(isolated) => patch({ forge: { isolated } })}
          />
        </Row>

        <Row
          label="Load all models"
          description="Load the full discovered catalog instead of only the premium tier (-all-models)."
        >
          <Toggle
            label="All models"
            checked={forgeSettings.allModels}
            onChange={(allModels) => patch({ forge: { allModels } })}
          />
        </Row>
      </div>

      <Row label="Run commands on this computer" description="Use your user account for trusted generated commands. Otherwise commands run in Docker.">
        <Toggle label="Native execution" checked={forgeSettings.native} onChange={(native) => patch({ forge: { native } })} />
      </Row>
    </div>
  )
}

// ---------------------------------------------------------------------------

function LogsSection({ patch }: { patch: Patch }) {
  const logs = useStudio((s) => s.settings!.logs)
  const held = useStudio((s) => s.logs.length)
  const truncated = useStudio((s) => s.logsTruncated)

  return (
    <div className="flex flex-col">
      <Row
        label="Buffer size"
        description="Records held in the main process. WorkerLog is roughly 77% of all runtime events, so a busy run fills this quickly."
      >
        <Input
          className="num w-32"
          type="number"
          min={1000}
          step={1000}
          value={logs.bufferSize}
          onChange={(event) =>
            patch({ logs: { bufferSize: Number(event.target.value) || 50_000 } })
          }
        />
      </Row>

      <Row
        label="Follow tail by default"
        description="Whether the log panel starts pinned to the newest record. Scrolling away always releases it regardless."
      >
        <Toggle
          label="Follow tail"
          checked={logs.followTail}
          onChange={(followTail) => patch({ logs: { followTail } })}
        />
      </Row>

      <div className="flex items-baseline gap-3 py-3.5 text-2xs text-fg-4">
        <span className="num">
          {held.toLocaleString()} records currently held in this window
        </span>
        {truncated ? (
          <span className="text-st-waiting">buffer has evicted older records</span>
        ) : null}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------

function AboutSection() {
  const settings = useStudio((s) => s.settings)
  const connection = useStudio((s) => s.connection)
  const forge = useStudio((s) => s.forge)
  const projection = useStudio((s) => s.projection)
  const platform = useStudio((s) => s.windowState.platform)

  return (
    <div className="flex flex-col">
      <AboutRow label="Studio" value={bridge?.version ?? 'unknown'} />
      <AboutRow label="Platform" value={platform} />
      <AboutRow label="Session" value={projection.sessionId ?? '—'} />
      <AboutRow label="Events seen" value={projection.eventCount.toLocaleString()} />
      <AboutRow label="Runs observed" value={String(projection.runOrder.length)} />
      <Divider />
      <AboutRow
        label="Orchestrator"
        value={`${connection.host}:${connection.port} · ${connection.phase}`}
      />
      <AboutRow label="Forge binary" value={settings?.forge.binaryPath || '—'} />
      <AboutRow label="Forge cwd" value={settings?.forge.cwd || '—'} />
      <AboutRow label="Last launch args" value={forge.args ? forge.args.join(' ') : '—'} />
    </div>
  )
}

// ---------------------------------------------------------------------------

function Row({
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
          <p className="pretty mt-0.5 text-2xs leading-relaxed text-fg-4">{description}</p>
        ) : null}
      </div>
      <div className="flex shrink-0 items-center justify-end">{children}</div>
    </div>
  )
}

function PathInput({
  value,
  onChange,
  pick,
}: {
  value: string
  onChange: (value: string) => void
  pick: () => Promise<string | null> | undefined
}) {
  return (
    <div className="flex w-full max-w-[380px] items-center gap-1">
      <Input
        className="mono w-full flex-1 text-2xs"
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
      <IconButton
        label="Browse"
        onClick={async () => {
          const picked = await pick()
          if (picked) onChange(picked)
        }}
      >
        <FolderOpen size={14} strokeWidth={1.7} />
      </IconButton>
    </div>
  )
}

function AboutRow({ label, value }: { label: string; value: string }) {
  return (
    <div className={cn('flex items-baseline gap-4 border-b border-line-1 py-2')}>
      <span className="w-36 shrink-0 text-xs text-fg-3">{label}</span>
      <span className="mono min-w-0 flex-1 text-2xs break-all text-fg-2">{value}</span>
    </div>
  )
}
