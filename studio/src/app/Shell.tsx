import { useEffect } from 'react'
import { AlertOctagon } from 'lucide-react'
import { EmptyState } from '@/design/primitives'
import { bridge } from '@/state/bridge'
import { useStudio } from '@/state/store'
import { useUi } from '@/state/ui'
import { ActivityRail } from './ActivityRail'
import { CommandPalette } from './CommandPalette'
import { DockPanel } from './DockPanel'
import { Resizer } from './Resizer'
import { SideBar } from './SideBar'
import { MainSurface } from './MainSurface'
import { StatusBar } from './StatusBar'
import { TitleBar } from './TitleBar'
import { isEnabled, matchKeybinding, runCommand } from './commands'

export function Shell() {
  const ready = useStudio((s) => s.ready)
  const bridgeMissing = useStudio((s) => s.bridgeMissing)
  const initialize = useStudio((s) => s.initialize)
  const settings = useStudio((s) => s.settings)

  const sidebarOpen = useUi((s) => s.sidebarOpen)
  const sidebarWidth = useUi((s) => s.sidebarWidth)
  const setSidebarWidth = useUi((s) => s.setSidebarWidth)
  const panelOpen = useUi((s) => s.panelOpen)
  const panelHeight = useUi((s) => s.panelHeight)
  const panelMaximized = useUi((s) => s.panelMaximized)
  const setPanelHeight = useUi((s) => s.setPanelHeight)
  const setPalette = useUi((s) => s.setPalette)

  useEffect(() => {
    void initialize()
  }, [initialize])

  // Native-menu commands resolve through the same registry as the palette.
  useEffect(() => {
    if (!bridge) return
    return bridge.onCommand((command) => runCommand(command))
  }, [])

  // Keybindings, likewise.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setPalette(false)
        return
      }
      const target = event.target as HTMLElement | null
      const typing =
        target?.tagName === 'INPUT' ||
        target?.tagName === 'TEXTAREA' ||
        target?.isContentEditable
      // Let the palette's own shortcut through even while typing.
      if (typing && !(event.ctrlKey || event.metaKey)) return

      const command = matchKeybinding(event)
      if (!command || !isEnabled(command)) return
      event.preventDefault()
      void command.run()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [setPalette])

  // Appearance preferences are applied as data attributes so the token layer
  // owns the actual values.
  useEffect(() => {
    const root = document.documentElement
    root.dataset.density = settings?.appearance.density ?? 'comfortable'
    root.dataset.reduceMotion = String(settings?.appearance.reduceMotion ?? false)
  }, [settings?.appearance.density, settings?.appearance.reduceMotion])

  if (bridgeMissing) return <BridgeMissing />
  if (!ready) return <div className="h-full w-full bg-bg-0" />

  return (
    <div className="flex h-full w-full flex-col overflow-hidden bg-bg-0">
      <TitleBar />

      <div className="flex min-h-0 flex-1">
        <ActivityRail />

        {sidebarOpen ? (
          <>
            <aside
              aria-label="Sidebar"
              className="flex min-h-0 shrink-0 flex-col border-r border-line-1 bg-bg-1"
              style={{ width: sidebarWidth }}
            >
              <SideBar />
            </aside>
            <Resizer
              orientation="vertical"
              label="Resize sidebar"
              onResize={(delta) => setSidebarWidth(sidebarWidth + delta)}
            />
          </>
        ) : null}

        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          {!panelMaximized ? (
            <div className="flex min-h-0 flex-1 flex-col">
              <MainSurface />
            </div>
          ) : null}

          {panelOpen ? (
            <>
              {!panelMaximized ? (
                <Resizer
                  orientation="horizontal"
                  label="Resize panel"
                  onResize={(delta) => setPanelHeight(panelHeight - delta)}
                />
              ) : null}
              <div
                className="flex min-h-0 flex-col"
                style={
                  panelMaximized
                    ? { flex: '1 1 0%' }
                    : { height: panelHeight, flexShrink: 0 }
                }
              >
                <DockPanel />
              </div>
            </>
          ) : null}
        </div>
      </div>

      <StatusBar />
      <CommandPalette />
    </div>
  )
}

/**
 * The failure mode this build exists to eliminate: if the preload does not
 * load, say so loudly instead of shipping an app whose buttons silently do
 * nothing.
 */
function BridgeMissing() {
  return (
    <div className="flex h-full w-full items-center justify-center bg-bg-0">
      <EmptyState
        icon={<AlertOctagon size={26} strokeWidth={1.4} />}
        title="Preload bridge failed to load"
        description={
          <>
            <code className="mono text-fg-2">window.reticle</code> is undefined, so Studio
            cannot reach the orchestrator, the filesystem, or the window controls. This
            usually means <code className="mono text-fg-2">dist-electron/preload.cjs</code>{' '}
            is missing or the path in <code className="mono text-fg-2">main.ts</code> does
            not match what the bundler emitted. Rebuild with{' '}
            <code className="mono text-fg-2">npm run build</code>.
          </>
        }
      />
    </div>
  )
}
