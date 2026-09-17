import { useCallback, useEffect, useState } from 'react'
import {
  ChevronDown,
  ChevronRight,
  File,
  FileClock,
  Files,
  Folder,
  FolderOpen,
  Folders,
  HardDrive,
  RefreshCw,
} from 'lucide-react'
import { cn } from '@/design/cn'
import { EmptyState, IconButton, SectionLabel, Spinner } from '@/design/primitives'
import { formatBytes, formatClock } from '@/design/status'
import { bridge } from '@/state/bridge'
import { openFileInEditor } from '@/state/actions'
import { useActiveRun } from '@/state/store'
import type { TreeEntry, WorkspaceSummary } from '@shared/ipc'

/** Active session workspace, backed by guarded main-process filesystem reads. */
export function ExplorerSidebar() {
  const run = useActiveRun()
  const [root, setRoot] = useState<TreeEntry[] | null>(null)
  const [summary, setSummary] = useState<WorkspaceSummary | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [revision, setRevision] = useState(0)

  const load = useCallback(async () => {
    if (!bridge) return
    setLoading(true)
    const summaryResult = await bridge.workspace.summary(run?.execId)
    if (!summaryResult.ok || !summaryResult.data) {
      setLoading(false)
      setRoot([])
      setSummary(null)
      setError(summaryResult.error ?? 'Could not inspect the workspace.')
      return
    }
    const nextSummary = summaryResult.data
    const treeResult = await bridge.workspace.tree(nextSummary.rootPath)
    setLoading(false)
    if (treeResult.ok && treeResult.data) {
      setSummary(nextSummary)
      setRoot(treeResult.data)
      setError(null)
      setRevision((value) => value + 1)
    } else {
      setError(treeResult.error ?? 'Could not read the workspace.')
    }
  }, [run?.execId])

  useEffect(() => {
    // The filesystem is an external system; refresh when the selected run changes.
    // oxlint-disable-next-line react/set-state-in-effect
    void load()
    if (run?.status !== 'running') return
    const timer = window.setInterval(() => void load(), 3_000)
    return () => window.clearInterval(timer)
  }, [load, run?.status])

  if (error && root === null) {
    return <EmptyState title="Workspace unavailable" description={error} />
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <SectionLabel>
        {run ? 'Active workspace' : 'Session workspaces'}
        <IconButton label="Refresh workspace" size="sm" className="ml-auto" onClick={load} disabled={loading}>
          {loading ? <Spinner size={10} /> : <RefreshCw size={11} strokeWidth={1.9} />}
        </IconButton>
      </SectionLabel>

      {summary ? <WorkspaceHeader summary={summary} runLabel={run?.execId} /> : null}

      {error ? (
        <div className="border-b border-line-1 bg-st-failed-weak px-3 py-2 text-2xs text-st-failed">{error}</div>
      ) : null}

      <div className="min-h-0 flex-1 overflow-auto pb-2">
        {summary && summary.recentFiles.length > 0 ? <RecentFiles files={summary.recentFiles} /> : null}

        <SectionLabel className="border-y border-line-1">
          <Folders size={11} className="mr-1.5" />
          Files
        </SectionLabel>

        {root === null ? (
          <div className="px-3 py-2 text-2xs text-fg-4">Loading…</div>
        ) : root.length === 0 ? (
          <EmptyState
            className="py-8"
            title={run ? 'Workspace is empty' : 'No sessions on disk'}
            description={run ? 'Generated agents, workflow definitions and source files will appear here as they are written.' : 'Isolated session workspaces appear once a run starts compiling.'}
          />
        ) : (
          root.map((entry) => <TreeRow key={entry.path} entry={entry} depth={0} refreshVersion={revision} />)
        )}
      </div>
    </div>
  )
}

function WorkspaceHeader({ summary, runLabel }: { summary: WorkspaceSummary; runLabel?: string }) {
  return (
    <div className="shrink-0 border-b border-line-1 bg-bg-0 px-3 py-2.5">
      <div className="flex items-center gap-2">
        <FolderOpen size={13} className="shrink-0 text-accent" />
        <span className="truncate-1 min-w-0 flex-1 text-xs font-medium text-fg-1">{runLabel ?? 'All sessions'}</span>
        <IconButton label="Reveal workspace in File Explorer" size="sm" onClick={() => void bridge?.workspace.reveal(summary.rootPath)}>
          <FolderOpen size={12} />
        </IconButton>
      </div>
      <div className="mono mt-1 truncate-1 text-[10px] text-fg-4" title={summary.rootPath}>{summary.rootPath}</div>
      <div className="mt-2 grid grid-cols-3 gap-1">
        <Metric icon={Files} label="Files" value={summary.fileCount.toLocaleString()} />
        <Metric icon={Folders} label="Folders" value={summary.directoryCount.toLocaleString()} />
        <Metric icon={HardDrive} label="Size" value={formatBytes(summary.totalBytes)} />
      </div>
      {summary.truncated ? <div className="mt-1.5 text-[10px] text-st-waiting">Summary capped at 20,000 entries.</div> : null}
    </div>
  )
}

function Metric({ icon: Icon, label, value }: { icon: typeof Files; label: string; value: string }) {
  return (
    <div className="rounded-[var(--radius-control)] border border-line-1 bg-bg-2 px-1.5 py-1">
      <div className="flex items-center gap-1 text-[10px] text-fg-4"><Icon size={9} /> {label}</div>
      <div className="num mt-0.5 text-xs text-fg-2">{value}</div>
    </div>
  )
}

function RecentFiles({ files }: { files: TreeEntry[] }) {
  return (
    <div>
      <SectionLabel><FileClock size={11} className="mr-1.5" />Recently changed</SectionLabel>
      {files.map((file) => (
        <button
          type="button"
          key={file.path}
          onClick={() => void openFileInEditor(file.path, file.name)}
          className="flex w-full items-center gap-1.5 px-3 py-1 text-left hover:bg-bg-2 focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-2"
          title={file.path}
        >
          <File size={10} className="shrink-0 text-fg-4" />
          <span className="truncate-1 min-w-0 flex-1 text-2xs text-fg-2">{file.name}</span>
          {file.modifiedAt ? <span className="num shrink-0 text-[10px] text-fg-4">{formatClock(file.modifiedAt)}</span> : null}
        </button>
      ))}
    </div>
  )
}

function TreeRow({ entry, depth, refreshVersion }: { entry: TreeEntry; depth: number; refreshVersion: number }) {
  const [open, setOpen] = useState(false)
  const [children, setChildren] = useState<TreeEntry[] | null>(null)

  const loadChildren = useCallback(async () => {
    if (!bridge || !entry.isDirectory) return
    const result = await bridge.workspace.tree(entry.path)
    setChildren(result.ok && result.data ? result.data : [])
  }, [entry.isDirectory, entry.path])

  useEffect(() => {
    // Expanded directories mirror external filesystem state during live refreshes.
    // oxlint-disable-next-line react/set-state-in-effect
    if (open) void loadChildren()
  }, [loadChildren, open, refreshVersion])

  const toggle = () => {
    if (!entry.isDirectory) {
      void openFileInEditor(entry.path, entry.name)
      return
    }
    setOpen((value) => !value)
  }

  return (
    <>
      <button
        type="button"
        onClick={toggle}
        className={cn('flex h-[var(--h-tree-row)] w-full items-center gap-1.5 pr-2 text-left','[transition-property:background-color] duration-[var(--dur-fast)]','hover:bg-bg-2 focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-2')}
        style={{ paddingLeft: 8 + depth * 12 }}
        title={entry.path}
      >
        {entry.isDirectory ? (open ? <ChevronDown size={11} strokeWidth={2} className="shrink-0 text-fg-4" /> : <ChevronRight size={11} strokeWidth={2} className="shrink-0 text-fg-4" />) : <span className="w-[11px] shrink-0" />}
        {entry.isDirectory ? <Folder size={11} strokeWidth={1.8} className="shrink-0 text-fg-4" /> : <File size={11} strokeWidth={1.8} className="shrink-0 text-fg-4" />}
        <span className="truncate-1 min-w-0 flex-1 text-xs text-fg-2">{entry.name}</span>
        {!entry.isDirectory && entry.size !== undefined ? <span className="num shrink-0 text-2xs text-fg-4">{formatBytes(entry.size)}</span> : null}
      </button>
      {open && children ? children.map((child) => <TreeRow key={child.path} entry={child} depth={depth + 1} refreshVersion={refreshVersion} />) : null}
    </>
  )
}
