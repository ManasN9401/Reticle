import { useEffect, useState, useRef } from 'react'
import { FolderOpen } from 'lucide-react'
import { Button, Input, Toggle } from '@/design/primitives'
import { useUi } from '@/state/ui'
import { useStudio } from '@/state/store'
import { bridge } from '@/state/bridge'
import type { ForgeStartRequest } from '@shared/ipc'

function PathInput({
  value,
  onChange,
  pick,
}: {
  value: string
  onChange: (value: string) => void
  pick: () => Promise<string | null>
}) {
  return (
    <div className="flex items-center gap-1">
      <Input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="flex-1"
        placeholder="Default (creates new session)"
      />
      {value ? (
        <Button size="sm" variant="subtle" onClick={() => onChange('')}>
          Clear
        </Button>
      ) : null}
      <Button
        size="sm"
        variant="subtle"
        icon={<FolderOpen size={14} />}
        onClick={async () => {
          const path = await pick()
          if (path) onChange(path)
        }}
      >
        Browse...
      </Button>
    </div>
  )
}

export function LaunchForgeDialog() {
  const open = useUi((s) => s.launcherOpen)
  return open ? <DialogContents /> : null
}

function DialogContents() {
  const setLauncherOpen = useUi((s) => s.setLauncherOpen)
  const settings = useStudio((s) => s.settings)

  const [port, setPort] = useState(settings?.connection.port ?? 8080)
  const [workspace, setWorkspace] = useState(settings?.forge.workspace ?? '')
  const [batch, setBatch] = useState(settings?.forge.batch ?? 5)
  const [isolated, setIsolated] = useState(settings?.forge.isolated ?? true)

  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    requestAnimationFrame(() => inputRef.current?.focus())
  }, [])

  const submit = async () => {
    setLauncherOpen(false)
    if (!bridge) return
    const request: ForgeStartRequest = {
      port,
      batch,
      isolated,
      workspace: workspace || undefined,
    }
    await bridge.forge.start(request)
  }

  return (
    <div
      className="fixed inset-0 z-[100] flex items-start justify-center bg-black/45 pt-[12vh]"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) setLauncherOpen(false)
      }}
    >
      <div
        role="dialog"
        aria-label="Launch Forge"
        className="flex w-[min(620px,calc(100vw-64px))] flex-col overflow-hidden rounded-[var(--radius-card)] border border-line-2 bg-bg-2 shadow-[var(--shadow-modal)]"
      >
        <header className="border-b border-line-1 px-5 py-4">
          <h2 className="text-sm font-medium text-fg-1">Launch Forge Configuration</h2>
          <p className="mt-1 text-xs text-fg-3">
            Configure this specific run. Leaves your global settings untouched.
          </p>
        </header>

        <div className="flex flex-col gap-5 px-5 py-5">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-fg-1">Port</label>
            <div className="text-2xs text-fg-4">
              Where the telemetry server is listening. Mismatches will cause reconnect loops.
            </div>
            <Input
              ref={inputRef}
              type="number"
              className="num w-28"
              value={port}
              onChange={(e) => setPort(Number(e.target.value) || 8080)}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-fg-1">Workspace</label>
            <div className="text-2xs text-fg-4">
              Existing workspace to resume from (-workspace). Leave blank for a fresh run.
            </div>
            <PathInput
              value={workspace}
              onChange={setWorkspace}
              pick={() => bridge?.workspace.pickDirectory() ?? Promise.resolve(null)}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-fg-1">Batch Size</label>
            <div className="text-2xs text-fg-4">
              Concurrent workflows (-batch). Caps how many queued items run at once.
            </div>
            <Input
              type="number"
              className="num w-28"
              value={batch}
              onChange={(e) => setBatch(Number(e.target.value) || 5)}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-fg-1">Isolated Environments</label>
            <div className="text-2xs text-fg-4">
              Compile ad-hoc binaries for each run vs reusing the same environment.
            </div>
            <Toggle
              label="Isolated"
              checked={isolated}
              onChange={setIsolated}
            />
          </div>
        </div>

        <footer className="flex justify-end gap-3 border-t border-line-1 bg-bg-1 px-5 py-3">
          <Button variant="subtle" onClick={() => setLauncherOpen(false)}>
            Cancel
          </Button>
          <Button variant="primary" onClick={submit}>
            Start Run
          </Button>
        </footer>
      </div>
    </div>
  )
}
