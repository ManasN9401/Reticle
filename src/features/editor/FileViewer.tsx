import Editor from '@monaco-editor/react'
import { useEffect, useState } from 'react'
import { ExternalLink, FolderOpen, Lock } from 'lucide-react'
import { EmptyState, IconButton, Spinner, Tooltip } from '@/design/primitives'
import { formatBytes } from '@/design/status'
import { bridge } from '@/state/bridge'
import { revealInExplorer } from '@/state/actions'
import { RETICLE_THEME, languageFor, setupMonaco } from './monacoSetup'
import type { ReadFileResult } from '@shared/ipc'

/**
 * Read-only source viewer.
 *
 * Deliberately read-only for now: the runtime holds file locks per session
 * (FileLockRequested / FileLockReleased) and there is no save path that would
 * respect them, so an editable buffer would be a lie about what Studio can
 * safely do to a running workspace.
 */
export function FileViewer({ path }: { path: string }) {
  const [file, setFile] = useState<ReadFileResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setupMonaco()
  }, [])

  useEffect(() => {
    if (!bridge) return
    let cancelled = false
    setFile(null)
    setError(null)
    void bridge.workspace.read(path).then((result) => {
      if (cancelled) return
      if (result.ok && result.data) setFile(result.data)
      else setError(result.error ?? 'Could not read this file.')
    })
    return () => {
      cancelled = true
    }
  }, [path])

  if (error) {
    return (
      <div className="h-full bg-inset">
        <EmptyState title="Could not open file" description={error} />
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-inset">
      <div className="flex h-[var(--h-toolbar)] shrink-0 items-center gap-2 border-b border-line-1 bg-bg-1 px-2">
        <Tooltip content="Read-only. Studio does not hold the runtime's file locks.">
          <span className="flex items-center gap-1 text-2xs text-fg-4">
            <Lock size={11} strokeWidth={1.8} />
            Read-only
          </span>
        </Tooltip>

        {file ? (
          <span className="num text-2xs text-fg-4">
            {formatBytes(file.size)}
            {file.truncated ? ' · truncated' : ''}
          </span>
        ) : null}

        <div className="ml-auto flex items-center gap-0.5">
          <IconButton
            label="Reveal in file explorer"
            size="sm"
            onClick={() => revealInExplorer(path)}
          >
            <FolderOpen size={13} strokeWidth={1.7} />
          </IconButton>
          <IconButton
            label="Open with the system default application"
            size="sm"
            onClick={() => bridge?.workspace.reveal(path)}
          >
            <ExternalLink size={13} strokeWidth={1.7} />
          </IconButton>
        </div>
      </div>

      <div className="min-h-0 flex-1">
        {file === null ? (
          <div className="flex h-full items-center justify-center gap-2 text-xs text-fg-3">
            <Spinner /> Opening…
          </div>
        ) : (
          <Editor
            height="100%"
            theme={RETICLE_THEME}
            language={languageFor(path)}
            value={file.content}
            options={{
              readOnly: true,
              domReadOnly: true,
              fontFamily: 'JetBrains Mono Variable, ui-monospace, monospace',
              fontSize: 12,
              lineHeight: 18,
              minimap: { enabled: true, renderCharacters: false, maxColumn: 80 },
              scrollBeyondLastLine: false,
              renderLineHighlight: 'line',
              smoothScrolling: true,
              padding: { top: 10, bottom: 10 },
              guides: { indentation: true },
              contextmenu: false,
              overviewRulerBorder: false,
              scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10 },
            }}
          />
        )}
      </div>
    </div>
  )
}
