import { useCallback, useEffect, useState } from 'react'
import { ChevronDown, ChevronRight, File, Folder, RefreshCw } from 'lucide-react'
import { cn } from '@/design/cn'
import { EmptyState, IconButton, SectionLabel, Spinner } from '@/design/primitives'
import { formatBytes } from '@/design/status'
import { bridge } from '@/state/bridge'
import { openFileInEditor } from '@/state/actions'
import type { TreeEntry } from '@shared/ipc'

/**
 * Session workspace explorer.
 *
 * Rooted at `.reticle/sessions` — the directory that actually holds a run's
 * generated agents, workers, workflow and source. Reads are guarded in the main
 * process against escaping the repository.
 */
export function ExplorerSidebar() {
  const [root, setRoot] = useState<TreeEntry[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    if (!bridge) return
    setLoading(true)
    const result = await bridge.workspace.tree()
    setLoading(false)
    if (result.ok && result.data) {
      setRoot(result.data)
      setError(null)
    } else {
      setError(result.error ?? 'Could not read the workspace.')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  if (error) {
    return <EmptyState title="Workspace unavailable" description={error} />
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <SectionLabel>
        Sessions
        <IconButton
          label="Refresh"
          size="sm"
          className="ml-auto"
          onClick={load}
          disabled={loading}
        >
          {loading ? <Spinner size={10} /> : <RefreshCw size={11} strokeWidth={1.9} />}
        </IconButton>
      </SectionLabel>

      <div className="min-h-0 flex-1 overflow-auto pb-2">
        {root === null ? (
          <div className="px-3 py-2 text-2xs text-fg-4">Loading…</div>
        ) : root.length === 0 ? (
          <EmptyState
            className="py-8"
            title="No sessions on disk"
            description="Isolated session workspaces appear under .reticle/sessions once a run compiles."
          />
        ) : (
          root.map((entry) => <TreeRow key={entry.path} entry={entry} depth={0} />)
        )}
      </div>
    </div>
  )
}

function TreeRow({ entry, depth }: { entry: TreeEntry; depth: number }) {
  const [open, setOpen] = useState(false)
  const [children, setChildren] = useState<TreeEntry[] | null>(null)

  const toggle = async () => {
    if (!entry.isDirectory) {
      void openFileInEditor(entry.path, entry.name)
      return
    }
    const next = !open
    setOpen(next)
    // Children are fetched lazily; the session tree can be deep and wide.
    if (next && children === null && bridge) {
      const result = await bridge.workspace.tree(entry.path)
      setChildren(result.ok && result.data ? result.data : [])
    }
  }

  return (
    <>
      <button
        type="button"
        onClick={toggle}
        className={cn(
          'flex h-[var(--h-tree-row)] w-full items-center gap-1.5 pr-2 text-left',
          '[transition-property:background-color] duration-[var(--dur-fast)]',
          'hover:bg-bg-2 focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-2',
        )}
        style={{ paddingLeft: 8 + depth * 12 }}
        title={entry.path}
      >
        {entry.isDirectory ? (
          open ? (
            <ChevronDown size={11} strokeWidth={2} className="shrink-0 text-fg-4" />
          ) : (
            <ChevronRight size={11} strokeWidth={2} className="shrink-0 text-fg-4" />
          )
        ) : (
          <span className="w-[11px] shrink-0" />
        )}
        {entry.isDirectory ? (
          <Folder size={11} strokeWidth={1.8} className="shrink-0 text-fg-4" />
        ) : (
          <File size={11} strokeWidth={1.8} className="shrink-0 text-fg-4" />
        )}
        <span className="truncate-1 min-w-0 flex-1 text-xs text-fg-2">{entry.name}</span>
        {!entry.isDirectory && entry.size !== undefined ? (
          <span className="num shrink-0 text-2xs text-fg-4">
            {formatBytes(entry.size)}
          </span>
        ) : null}
      </button>

      {open && children
        ? children.map((child) => (
            <TreeRow key={child.path} entry={child} depth={depth + 1} />
          ))
        : null}
    </>
  )
}
