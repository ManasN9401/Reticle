import { useCallback, useEffect, useState } from 'react'
import { FolderOpen, RefreshCw, Trash2 } from 'lucide-react'
import { Badge, Button, Chip, EmptyState, Spinner, Toggle } from '@/design/primitives'
import { bridge } from '@/state/bridge'
import { useStudio } from '@/state/store'
import type { PluginView } from '@shared/ipc'

export function PluginsSection() {
  const connected = useStudio((state) => state.connection.phase === 'connected')
  const [plugins, setPlugins] = useState<PluginView[] | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const load = useCallback(async () => {
    if (!bridge || !connected) return
    const result = await bridge.api.plugins()
    if (result.ok && result.data) { setPlugins(result.data); setError(null) }
    else setError(result.error ?? 'Could not read plugins.')
  }, [connected])
  useEffect(() => { if (connected) void load() }, [connected, load])
  const install = async () => {
    if (!bridge) return
    const path = await bridge.workspace.pickDirectory()
    if (!path) return
    setBusy('install')
    const result = await bridge.api.installPlugin(path)
    setBusy(null)
    if (!result.ok) setError(result.error ?? 'Plugin installation failed.')
    await load()
  }
  const act = async (id: string, action: 'enable' | 'disable') => {
    if (!bridge) return
    setBusy(`${id}:${action}`)
    const result = await bridge.api.actOnPlugin(id, action)
    setBusy(null)
    if (!result.ok) setError(result.error ?? `${action} failed.`)
    await load()
  }
  const remove = async (id: string) => {
    if (!bridge) return
    setBusy(`${id}:delete`)
    const result = await bridge.api.deletePlugin(id)
    setBusy(null)
    if (!result.ok) setError(result.error ?? 'Delete failed.')
    await load()
  }

  if (!connected) return <EmptyState title="Not connected" description="Plugin state is owned by the running Reticle runtime. Start or connect to Forge to manage it." />
  if (plugins === null) return <div className="flex items-center gap-2 py-6 text-xs text-fg-3"><Spinner /> Loading plugins…</div>
  return <div className="flex min-w-0 flex-col gap-4">
    <div className="flex flex-wrap items-center justify-between gap-3"><p className="min-w-[16rem] flex-1 text-xs text-fg-3">Plugins are explicitly installed local bundles. Enabling is transactional: invalid compatibility, dependencies, identities, tools or MCP declarations leave the live registries unchanged.</p><div className="flex flex-wrap gap-2"><Button size="sm" icon={<RefreshCw size={13} />} onClick={load}>Refresh</Button><Button size="sm" variant="primary" icon={<FolderOpen size={13} />} onClick={install} disabled={busy !== null}>{busy === 'install' ? <Spinner size={12} /> : null}Install folder</Button></div></div>
    {error ? <p className="rounded border border-st-failed/40 bg-st-failed-weak px-3 py-2 text-xs text-st-failed">{error}</p> : null}
    {plugins.length === 0 ? <EmptyState title="No plugins installed" description="Choose a folder containing a plugin.json manifest. Installation copies and validates the bundle; it does not enable it automatically." /> : null}
    <div className="grid min-w-0 gap-3 lg:grid-cols-2">{plugins.map((plugin) => <div key={plugin.id} className="min-w-0 overflow-hidden rounded-[var(--radius-card)] border border-line-2 bg-bg-1 p-4">
      <div className="flex min-w-0 items-start justify-between gap-3"><div className="min-w-0 flex-1"><div className="flex min-w-0 flex-wrap items-center gap-2"><span className="truncate-1 max-w-full text-sm font-medium text-fg-1">{plugin.id}</span><Chip>{plugin.version}</Chip><Badge>{plugin.valid ? (plugin.enabled ? 'enabled' : 'disabled') : 'invalid'}</Badge>{plugin.restartRequired ? <Badge>restart</Badge> : null}</div>{plugin.description ? <p className="mt-1 break-words text-xs text-fg-3">{plugin.description}</p> : null}</div><Toggle label={`${plugin.enabled ? 'Disable' : 'Enable'} ${plugin.id}`} checked={plugin.enabled} disabled={!plugin.valid || busy !== null} onChange={(enabled) => void act(plugin.id, enabled ? 'enable' : 'disable')} /></div>
      {plugin.error ? <p className="mt-3 text-xs text-st-failed">{plugin.error}</p> : null}
      <div className="mt-3 grid grid-cols-2 gap-2 text-2xs"><Contribution label="Agents" values={plugin.agents} /><Contribution label="Skills" values={plugin.skills} /><Contribution label="Tools" values={plugin.tools} /><Contribution label="MCP servers" values={plugin.mcpServers} /></div>
      {plugin.dependencies.length ? <p className="mt-3 text-2xs text-fg-4">Depends on {plugin.dependencies.map((dependency) => `${dependency.plugin} ${dependency.versionRange}`).join(', ')}</p> : null}
      <div className="mt-4"><Button size="sm" variant="danger" icon={<Trash2 size={12} />} disabled={plugin.enabled || busy !== null} onClick={() => void remove(plugin.id)}>Delete</Button></div>
    </div>)}</div>
  </div>
}

function Contribution({ label, values }: { label: string; values: string[] }) {
  return <div className="min-w-0 overflow-hidden rounded bg-bg-2 p-2"><span className="text-fg-4">{label}</span><div className="mt-1 flex min-w-0 flex-wrap gap-1">{values.length ? values.map((value) => <Chip key={value}>{value}</Chip>) : <span className="text-fg-4">None</span>}</div></div>
}
