import { useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { cn } from '@/design/cn'
import { Button, IconButton, Input } from '@/design/primitives'
import { useStudio } from '@/state/store'
import { useUi } from '@/state/ui'
import type { EditorTab, LayoutSnapshot, NamedLayout, SettingsPatch } from '@shared/ipc'

function newLayoutId(): string {
  return `layout-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/**
 * Named, switchable saved arrangements of the sidebar/panel/tab layout.
 * Separate from the *current* arrangement, which is auto-persisted in the
 * background regardless (see `state/useLayoutPersistence.ts`) — there's no
 * toggle for that here because it's just always on.
 */
export function TabLayoutsSection({
  patch,
}: {
  patch: (next: SettingsPatch) => Promise<void>
}) {
  const tabLayouts = useStudio((s) => s.settings!.tabLayouts)
  const ui = useUi()
  const [name, setName] = useState('')

  async function saveCurrent() {
    const trimmed = name.trim()
    if (!trimmed) return
    // Named layouts drop inline-content tabs (API-delivered, execution-scoped
    // output) — persisting arbitrary file bodies into settings.json would
    // bloat it, and that content may not even be meaningful later. Auto-persist
    // keeps them since it's overwritten continuously anyway.
    const tabs: EditorTab[] = ui.tabs
      .filter((tab) => tab.kind === 'graph' || tab.content === undefined)
      .map(({ id, kind, title, path, subtitle }) => ({ id, kind, title, path, subtitle }))
    const snapshot: LayoutSnapshot = {
      activeView: ui.activeView,
      sidebarOpen: ui.sidebarOpen,
      sidebarWidth: ui.sidebarWidth,
      panelOpen: ui.panelOpen,
      panelTab: ui.panelTab,
      panelHeight: ui.panelHeight,
      panelMaximized: ui.panelMaximized,
      tabs: tabs.length > 0 ? tabs : [{ id: 'graph', kind: 'graph', title: 'Node Map' }],
      activeTabId: tabs.some((t) => t.id === ui.activeTabId) ? ui.activeTabId : 'graph',
      inspectorOpen: ui.inspectorOpen,
    }
    const layout: NamedLayout = { id: newLayoutId(), name: trimmed, snapshot }
    await patch({ tabLayouts: { autoPersist: tabLayouts.autoPersist, saved: [...tabLayouts.saved, layout] } })
    setName('')
  }

  async function renameLayout(id: string, next: string) {
    await patch({
      tabLayouts: {
        autoPersist: tabLayouts.autoPersist,
        saved: tabLayouts.saved.map((l) => (l.id === id ? { ...l, name: next } : l)),
      },
    })
  }

  async function deleteLayout(id: string) {
    await patch({
      tabLayouts: {
        autoPersist: tabLayouts.autoPersist,
        saved: tabLayouts.saved.filter((l) => l.id !== id),
      },
    })
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="pretty text-2xs leading-relaxed text-fg-4">
        Your current arrangement of sidebar, panels and tabs is remembered automatically and
        restored on the next launch — nothing to configure for that. Save named arrangements
        below to switch between them (e.g. one with Problems and Logs open for debugging, another
        with just the explorer and editor).
      </p>

      <div className="flex items-center gap-1.5">
        <Input
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="Name this arrangement…"
          className="h-8 w-56 text-xs"
        />
        <Button
          size="sm"
          variant="primary"
          icon={<Plus size={12} strokeWidth={1.8} />}
          onClick={saveCurrent}
          disabled={!name.trim()}
        >
          Save current as…
        </Button>
      </div>

      {tabLayouts.saved.length === 0 ? (
        <div className="pretty rounded-[var(--radius-card)] border border-dashed border-line-2 px-3 py-4 text-center text-xs text-fg-4">
          No saved layouts yet.
        </div>
      ) : (
        <div className="flex flex-col gap-1.5">
          {tabLayouts.saved.map((layout) => (
            <div
              key={layout.id}
              className={cn(
                'flex items-center gap-2 rounded-[var(--radius-control)] border border-line-2 px-2 py-1.5',
              )}
            >
              <Input
                value={layout.name}
                onChange={(event) => renameLayout(layout.id, event.target.value)}
                className="h-6 w-48 text-xs"
              />
              <span className="text-2xs text-fg-4">{layout.snapshot.tabs.length} tab(s)</span>
              <div className="ml-auto flex shrink-0 items-center gap-1">
                <Button size="sm" onClick={() => useUi.getState().restoreLayout(layout.snapshot)}>
                  Apply
                </Button>
                <IconButton
                  label="Delete layout"
                  size="sm"
                  onClick={() => deleteLayout(layout.id)}
                >
                  <Trash2 size={13} strokeWidth={1.7} />
                </IconButton>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
