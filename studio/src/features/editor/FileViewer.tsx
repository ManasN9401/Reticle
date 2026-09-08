import Editor from '@monaco-editor/react'
import { useEffect, useState } from 'react'
import { FolderOpen, Lock } from 'lucide-react'
import { EmptyState, IconButton, Spinner, Tooltip } from '@/design/primitives'
import { formatBytes } from '@/design/status'
import { bridge } from '@/state/bridge'
import { revealInExplorer } from '@/state/actions'
import { useResolvedTheme } from '@/state/theme'
import { RETICLE_DARK, RETICLE_LIGHT, languageFor, setupMonaco } from './monacoSetup'

/**
 * Read-only source viewer.
 *
 * Deliberately read-only: workspace writes belong to workers and their file tools.
 *
 * Content comes either from disk (via the guarded main-process reader) or
 * inline, for files delivered by `GET /api/outputs/{execId}`.
 */
export function FileViewer({
  path,
  inlineContent,
  label,
}: {
  path?: string
  inlineContent?: string
  label: string
}) {
  const [loaded, setLoaded] = useState<{path: string, content: string|null, size?: number, truncated?: boolean, error?: string} | null>(null)
  const current = loaded?.path === path ? loaded : null
  const content = inlineContent ?? current?.content ?? null
  const size = inlineContent !== undefined ? new TextEncoder().encode(inlineContent).length : current?.size
  const truncated = inlineContent === undefined && current?.truncated
  const error = inlineContent === undefined ? current?.error : null
  const theme = useResolvedTheme()

  // Re-run on theme change so both palettes exist before Monaco is asked for one.
  useEffect(() => {
    setupMonaco()
  }, [theme])

  useEffect(() => {
    if (inlineContent !== undefined || !path || !bridge) return
    let cancelled = false
    void bridge.workspace.read(path).then(result => {
      if (cancelled) return
      if (result.ok && result.data) setLoaded({...result.data, path})
      else setLoaded({path, content:null, error:result.error ?? 'Could not read this file.'})
    })
    return () => {
      cancelled = true
    }
  }, [path, inlineContent])

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
        <Tooltip content="Read-only — workspace edits belong to the executing workers.">
          <span className="flex items-center gap-1 text-2xs text-fg-4">
            <Lock size={11} strokeWidth={1.8} />
            Read-only
          </span>
        </Tooltip>

        <span className="num text-2xs text-fg-4">
          {formatBytes(size)}
          {truncated ? ' · truncated' : ''}
        </span>

        {path ? (
          <IconButton
            label="Reveal in file explorer"
            size="sm"
            className="ml-auto"
            onClick={() => revealInExplorer(path)}
          >
            <FolderOpen size={13} strokeWidth={1.7} />
          </IconButton>
        ) : null}
      </div>

      <div className="min-h-0 flex-1">
        {content === null ? (
          <div className="flex h-full items-center justify-center gap-2 text-xs text-fg-3">
            <Spinner /> Opening…
          </div>
        ) : (
          <Editor
            height="100%"
            theme={theme === 'light' ? RETICLE_LIGHT : RETICLE_DARK}
            language={languageFor(path ?? label)}
            value={content}
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
