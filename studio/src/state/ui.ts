import { create } from 'zustand'
import type { EditorTab, LayoutSnapshot, PanelTab, ViewId } from '@shared/ipc'

/**
 * `ViewId`/`PanelTab`/`EditorTab` live in `@shared/ipc` now — a saved or
 * auto-persisted layout crosses into the main process via settings.json, so
 * these needed a shared home. Re-exported here so existing imports of them
 * from `@/state/ui` keep working unchanged.
 */
export type { ViewId, PanelTab, EditorTab }

const GRAPH_TAB: EditorTab = { id: 'graph', kind: 'graph', title: 'Node Map' }

export interface UiStore {
  activeView: ViewId
  sidebarOpen: boolean
  sidebarWidth: number

  panelOpen: boolean
  panelTab: PanelTab
  panelHeight: number
  panelMaximized: boolean

  tabs: EditorTab[]
  activeTabId: string

  paletteOpen: boolean
  launcherOpen: boolean
  inspectorOpen: boolean

  /** Node id whose approval checkpoint is being reviewed. */
  reviewNodeId: string | null

  setView(view: ViewId): void
  toggleSidebar(): void
  setSidebarWidth(width: number): void

  togglePanel(): void
  setPanelTab(tab: PanelTab): void
  setPanelHeight(height: number): void
  togglePanelMaximized(): void

  openTab(tab: EditorTab): void
  closeTab(id: string): void
  setActiveTab(id: string): void

  setPalette(open: boolean): void
  setLauncherOpen(open: boolean): void
  toggleInspector(): void
  setReviewNode(nodeId: string | null): void

  /** Applies a persisted arrangement (auto-persist on startup, or a saved layout). */
  restoreLayout(snapshot: LayoutSnapshot): void
}

export const useUi = create<UiStore>((set, get) => ({
  activeView: 'runs',
  sidebarOpen: true,
  sidebarWidth: 288,

  panelOpen: true,
  panelTab: 'activity',
  panelHeight: 260,
  panelMaximized: false,

  tabs: [GRAPH_TAB],
  activeTabId: GRAPH_TAB.id,

  paletteOpen: false,
  launcherOpen: false,
  inspectorOpen: true,
  reviewNodeId: null,

  setView(view) {
    // Clicking the already-active rail icon collapses the sidebar, matching
    // the behaviour every editor with an activity bar has.
    const { activeView, sidebarOpen } = get()
    if (activeView === view && sidebarOpen) {
      set({ sidebarOpen: false })
      return
    }
    set({ activeView: view, sidebarOpen: true })
  },

  toggleSidebar() {
    set((s) => ({ sidebarOpen: !s.sidebarOpen }))
  },

  setSidebarWidth(width) {
    set({ sidebarWidth: Math.min(560, Math.max(200, Math.round(width))) })
  },

  togglePanel() {
    set((s) => ({ panelOpen: !s.panelOpen, panelMaximized: false }))
  },

  setPanelTab(tab) {
    set({ panelTab: tab, panelOpen: true })
  },

  setPanelHeight(height) {
    set({ panelHeight: Math.min(900, Math.max(120, Math.round(height))) })
  },

  togglePanelMaximized() {
    set((s) => ({ panelMaximized: !s.panelMaximized, panelOpen: true }))
  },

  openTab(tab) {
    const { tabs } = get()
    if (!tabs.some((t) => t.id === tab.id)) {
      set({ tabs: [...tabs, tab] })
    }
    set({ activeTabId: tab.id })
  },

  closeTab(id) {
    // The graph is the app's home surface; it is not closable.
    if (id === GRAPH_TAB.id) return
    const { tabs, activeTabId } = get()
    const index = tabs.findIndex((t) => t.id === id)
    if (index === -1) return
    const next = tabs.filter((t) => t.id !== id)
    const nextActive =
      activeTabId === id ? (next[index - 1] ?? next[0] ?? GRAPH_TAB).id : activeTabId
    set({ tabs: next.length > 0 ? next : [GRAPH_TAB], activeTabId: nextActive })
  },

  setActiveTab(id) {
    set({ activeTabId: id })
  },

  setPalette(open) {
    set({ paletteOpen: open })
  },

  setLauncherOpen(open) {
    set({ launcherOpen: open })
  },

  toggleInspector() {
    set((s) => ({ inspectorOpen: !s.inspectorOpen }))
  },

  setReviewNode(nodeId) {
    set({ reviewNodeId: nodeId })
  },

  restoreLayout(snapshot) {
    set({
      activeView: snapshot.activeView,
      sidebarOpen: snapshot.sidebarOpen,
      sidebarWidth: snapshot.sidebarWidth,
      panelOpen: snapshot.panelOpen,
      panelTab: snapshot.panelTab,
      panelHeight: snapshot.panelHeight,
      panelMaximized: snapshot.panelMaximized,
      tabs: snapshot.tabs.length > 0 ? snapshot.tabs : [GRAPH_TAB],
      activeTabId: snapshot.activeTabId,
      inspectorOpen: snapshot.inspectorOpen,
    })
  },
}))
