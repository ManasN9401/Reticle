import { lazy, Suspense, useState } from 'react'
import { GraphCanvas } from '@/features/graph/GraphCanvas'
import { Inspector } from '@/features/graph/Inspector'
import { ReviewSheet } from '@/features/hitl/ReviewSheet'
import { SettingsView } from '@/features/settings/SettingsView'
import { Spinner } from '@/design/primitives'
import { useUi } from '@/state/ui'
import { Resizer } from './Resizer'
import { ViewTabs } from './ViewTabs'

/**
 * Monaco is by far the heaviest dependency in the bundle. Loading it lazily
 * keeps the shell and the node map — the surfaces that matter on launch — off
 * the critical path, since many sessions never open a file at all.
 */
const FileViewer = lazy(() =>
  import('@/features/editor/FileViewer').then((m) => ({ default: m.FileViewer })),
)

/**
 * The main editing surface: tab strip, the active tab's content, and the node
 * inspector docked to its right.
 */
export function MainSurface() {
  const activeView = useUi((s) => s.activeView)
  const tabs = useUi((s) => s.tabs)
  const activeTabId = useUi((s) => s.activeTabId)
  const inspectorOpen = useUi((s) => s.inspectorOpen)
  const reviewNodeId = useUi((s) => s.reviewNodeId)
  const [inspectorWidth, setInspectorWidth] = useState(320)

  // Settings takes over the whole surface rather than living in a tab: it is a
  // mode, not a document.
  if (activeView === 'settings') {
    return <SettingsView />
  }

  const activeTab = tabs.find((t) => t.id === activeTabId) ?? tabs[0]

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <ViewTabs />

      <div className="flex min-h-0 flex-1">
        <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
          {activeTab?.kind === 'file' ? (
            <Suspense
              fallback={
                <div className="flex h-full items-center justify-center gap-2 bg-inset text-xs text-fg-3">
                  <Spinner /> Loading editor…
                </div>
              }
            >
              <FileViewer
                key={activeTab.id}
                path={activeTab.path}
                inlineContent={activeTab.content}
                label={activeTab.subtitle ?? activeTab.title}
              />
            </Suspense>
          ) : (
            <GraphCanvas />
          )}
          {reviewNodeId ? <ReviewSheet nodeId={reviewNodeId} /> : null}
        </div>

        {inspectorOpen ? (
          <>
            <Resizer
              orientation="vertical"
              label="Resize inspector"
              onResize={(delta) =>
                setInspectorWidth((w) => Math.min(560, Math.max(240, w - delta)))
              }
            />
            <aside
              aria-label="Inspector"
              className="flex min-h-0 shrink-0 flex-col border-l border-line-1 bg-bg-1"
              style={{ width: inspectorWidth }}
            >
              <Inspector />
            </aside>
          </>
        ) : null}
      </div>
    </div>
  )
}
