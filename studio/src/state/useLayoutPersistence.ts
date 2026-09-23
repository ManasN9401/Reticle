import { useEffect, useRef } from 'react'
import type { LayoutSnapshot } from '@shared/ipc'
import { bridge } from './bridge'
import { useStudio } from './store'
import { useUi } from './ui'

const PERSIST_DEBOUNCE_MS = 800

/**
 * Restores the last-used sidebar/panel/tab arrangement once settings finish
 * loading, then keeps it in sync afterward — debounced, since dragging a
 * resizer fires many updates and settings.json is written synchronously on
 * every patch.
 *
 * Mount once, near the shell root (alongside `useApplyTheme`).
 */
export function useLayoutPersistence(): void {
  const settings = useStudio((s) => s.settings)
  const restoreLayout = useUi((s) => s.restoreLayout)

  const activeView = useUi((s) => s.activeView)
  const sidebarOpen = useUi((s) => s.sidebarOpen)
  const sidebarWidth = useUi((s) => s.sidebarWidth)
  const panelOpen = useUi((s) => s.panelOpen)
  const panelTab = useUi((s) => s.panelTab)
  const panelHeight = useUi((s) => s.panelHeight)
  const panelMaximized = useUi((s) => s.panelMaximized)
  const tabs = useUi((s) => s.tabs)
  const activeTabId = useUi((s) => s.activeTabId)
  const inspectorOpen = useUi((s) => s.inspectorOpen)

  const hydrated = useRef(false)

  // One-time hydration, the first moment settings are available.
  useEffect(() => {
    if (hydrated.current || !settings) return
    hydrated.current = true
    restoreLayout(settings.tabLayouts.autoPersist)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentionally runs once per settings-becomes-available transition, not on every settings change
  }, [Boolean(settings)])

  // Debounced auto-persist of whatever the arrangement is now.
  useEffect(() => {
    if (!hydrated.current || !settings || !bridge) return
    const snapshot: LayoutSnapshot = {
      activeView,
      sidebarOpen,
      sidebarWidth,
      panelOpen,
      panelTab,
      panelHeight,
      panelMaximized,
      tabs,
      activeTabId,
      inspectorOpen,
    }
    const timer = setTimeout(() => {
      void bridge?.settings.patch({
        tabLayouts: { autoPersist: snapshot, saved: settings.tabLayouts.saved },
      })
    }, PERSIST_DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [
    activeView,
    sidebarOpen,
    sidebarWidth,
    panelOpen,
    panelTab,
    panelHeight,
    panelMaximized,
    tabs,
    activeTabId,
    inspectorOpen,
    settings,
  ])
}
