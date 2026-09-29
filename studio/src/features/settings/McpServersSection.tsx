import { useCallback, useEffect, useState } from 'react'
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { Badge, Button, Chip, EmptyState, Input, Spinner, Toggle } from '@/design/primitives'
import { bridge } from '@/state/bridge'
import { useStudio } from '@/state/store'
import type { McpServerConfig, McpServerStatus } from '@shared/ipc'

const blank: McpServerConfig = {
  id: '', command: '', args: [], env: {}, enabled: true, transport: 'stdio',
  startupTimeoutSeconds: 15, callTimeoutSeconds: 30,
}

function lines(values: string[]) { return values.join('\n') }
function parseLines(value: string) { return value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean) }
function envLines(values: Record<string, string>) { return Object.entries(values).map(([child, host]) => `${child}=${host}`).join('\n') }
function parseEnv(value: string) {
  const result: Record<string, string> = {}
  for (const line of parseLines(value)) {
    const split = line.indexOf('=')
    if (split > 0) result[line.slice(0, split).trim()] = line.slice(split + 1).trim()
  }
  return result
}

export function McpServersSection() {
  const connected = useStudio((state) => state.connection.phase === 'connected')
  const [servers, setServers] = useState<McpServerStatus[] | null>(null)
  const [editing, setEditing] = useState<McpServerConfig | null>(null)
  const [args, setArgs] = useState('')
  const [env, setEnv] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!bridge || !connected) return
    const result = await bridge.api.mcpServers()
    if (result.ok && result.data) { setServers(result.data); setError(null) }
    else setError(result.error ?? 'Could not read MCP servers.')
  }, [connected])

  useEffect(() => { if (connected) void load() }, [connected, load])

  const edit = (config: McpServerConfig) => {
    setEditing({ ...config, args: [...config.args], env: { ...config.env } })
    setArgs(lines(config.args)); setEnv(envLines(config.env)); setError(null)
  }
  const act = async (id: string, action: 'enable' | 'disable' | 'test') => {
    if (!bridge) return
    setBusy(`${id}:${action}`)
    const result = await bridge.api.actOnMcpServer(id, action)
    setBusy(null)
    if (!result.ok) setError(result.error ?? `${action} failed.`)
    await load()
  }
  const save = async () => {
    if (!bridge || !editing) return
    setBusy('save')
    const result = await bridge.api.saveMcpServer({ ...editing, args: parseLines(args), env: parseEnv(env) })
    setBusy(null)
    if (!result.ok) { setError(result.error ?? 'Save failed.'); return }
    setEditing(null); await load()
  }
  const remove = async (id: string) => {
    if (!bridge) return
    setBusy(`${id}:delete`)
    const result = await bridge.api.deleteMcpServer(id)
    setBusy(null)
    if (!result.ok) setError(result.error ?? 'Delete failed.')
    await load()
  }

  if (!connected) return <EmptyState title="Not connected" description="MCP state is owned by the running Reticle runtime. Start or connect to Forge to manage it." />
  if (servers === null) return <div className="flex items-center gap-2 py-6 text-xs text-fg-3"><Spinner /> Loading MCP servers…</div>

  return <div className="flex min-w-0 flex-col gap-4">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <p className="min-w-[16rem] flex-1 text-xs text-fg-3">Only local stdio servers are supported. Environment entries map child names to host variable names, so secret values never enter Reticle’s saved configuration.</p>
      <div className="flex flex-wrap gap-2"><Button size="sm" icon={<RefreshCw size={13} />} onClick={load}>Refresh</Button><Button size="sm" variant="primary" icon={<Plus size={13} />} onClick={() => edit(blank)}>Add server</Button></div>
    </div>
    {error ? <p className="rounded border border-st-failed/40 bg-st-failed-weak px-3 py-2 text-xs text-st-failed">{error}</p> : null}
    {editing ? <div className="grid min-w-0 gap-3 rounded-[var(--radius-card)] border border-line-2 bg-bg-1 p-4 md:grid-cols-2">
      <label className="flex min-w-0 flex-col gap-1 text-xs text-fg-2">Server id<Input className="min-w-0 w-full" value={editing.id} disabled={servers.some((server) => server.id === editing.id)} onChange={(event) => setEditing({ ...editing, id: event.target.value })} /></label>
      <label className="flex min-w-0 flex-col gap-1 text-xs text-fg-2">Command<Input className="min-w-0 w-full" value={editing.command} onChange={(event) => setEditing({ ...editing, command: event.target.value })} /></label>
      <label className="flex min-w-0 flex-col gap-1 text-xs text-fg-2">Working directory (inside checkout)<Input className="min-w-0 w-full" value={editing.cwd ?? ''} placeholder="Optional" onChange={(event) => setEditing({ ...editing, cwd: event.target.value || undefined })} /></label>
      <div className="grid min-w-0 grid-cols-[repeat(2,minmax(0,1fr))] gap-2"><label className="flex min-w-0 flex-col gap-1 text-xs text-fg-2">Startup seconds<Input className="min-w-0 w-full" type="number" min={1} max={120} value={editing.startupTimeoutSeconds} onChange={(event) => setEditing({ ...editing, startupTimeoutSeconds: Number(event.target.value) })} /></label><label className="flex min-w-0 flex-col gap-1 text-xs text-fg-2">Call seconds<Input className="min-w-0 w-full" type="number" min={1} max={7200} value={editing.callTimeoutSeconds} onChange={(event) => setEditing({ ...editing, callTimeoutSeconds: Number(event.target.value) })} /></label></div>
      <label className="flex min-w-0 flex-col gap-1 text-xs text-fg-2">Arguments — one per line<textarea className="min-h-24 min-w-0 w-full rounded border border-line-2 bg-inset p-2 font-mono text-xs text-fg-1" value={args} onChange={(event) => setArgs(event.target.value)} /></label>
      <label className="flex min-w-0 flex-col gap-1 text-xs text-fg-2">Environment — CHILD_NAME=HOST_NAME<textarea className="min-h-24 min-w-0 w-full rounded border border-line-2 bg-inset p-2 font-mono text-xs text-fg-1" value={env} onChange={(event) => setEnv(event.target.value)} /></label>
      <div className="col-span-full flex flex-wrap items-center justify-between gap-3"><label className="flex items-center gap-2 text-xs text-fg-2"><Toggle label="Enable after saving" checked={editing.enabled} onChange={(enabled) => setEditing({ ...editing, enabled })} />Enable after saving</label><div className="flex gap-2"><Button size="sm" variant="subtle" onClick={() => setEditing(null)}>Cancel</Button><Button size="sm" variant="primary" disabled={busy === 'save' || !editing.id || !editing.command} onClick={save}>{busy === 'save' ? <Spinner size={12} /> : null}Save</Button></div></div>
    </div> : null}
    {servers.length === 0 && !editing ? <EmptyState title="No MCP servers" description="Add an explicitly trusted local stdio server. Reticle will discover its tools and gate every call through the attempt-scoped broker." /> : null}
    <div className="grid min-w-0 gap-3 lg:grid-cols-2">{servers.map((server) => <div key={server.id} className="min-w-0 overflow-hidden rounded-[var(--radius-card)] border border-line-2 bg-bg-1 p-4">
      <div className="flex min-w-0 items-start justify-between gap-3"><div className="min-w-0 flex-1"><div className="flex min-w-0 flex-wrap items-center gap-2"><span className="truncate-1 max-w-full font-mono text-sm text-fg-1">{server.id}</span><Badge>{server.state}</Badge>{server.source === 'plugin' ? <Badge>plugin</Badge> : null}</div><p className="mt-1 truncate font-mono text-2xs text-fg-4" title={`${server.config.command} ${server.config.args.join(' ')}`}>{server.config.command} {server.config.args.join(' ')}</p></div><Toggle label={`${server.config.enabled ? 'Disable' : 'Enable'} ${server.id}`} checked={server.config.enabled} disabled={server.source === 'plugin' || busy !== null} onChange={(enabled) => void act(server.id, enabled ? 'enable' : 'disable')} /></div>
      {server.lastError ? <p className="mt-3 text-xs text-st-failed">{server.lastError}</p> : null}
      {server.missingVariables?.length ? <p className="mt-2 text-xs text-st-waiting">Missing host variables: {server.missingVariables.join(', ')}</p> : null}
      <div className="mt-3 flex flex-wrap gap-1">{server.tools.map((tool) => <Chip key={tool.name} title={tool.description}>{tool.name}</Chip>)}{server.tools.length === 0 ? <span className="text-2xs text-fg-4">No tools discovered</span> : null}</div>
      <div className="mt-4 flex flex-wrap gap-2"><Button size="sm" onClick={() => void act(server.id, 'test')} disabled={busy !== null}>Test & discover</Button>{server.source === 'standalone' ? <><Button size="sm" icon={<Pencil size={12} />} onClick={() => edit(server.config)}>Edit</Button><Button size="sm" variant="danger" icon={<Trash2 size={12} />} onClick={() => void remove(server.id)}>Delete</Button></> : null}</div>
    </div>)}</div>
  </div>
}
