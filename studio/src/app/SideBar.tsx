import { SectionLabel } from '@/design/primitives'
import { AgentsSidebar } from '@/features/agents/AgentsSidebar'
import { ArtifactsSidebar } from '@/features/artifacts/ArtifactsSidebar'
import { ExplorerSidebar } from '@/features/explorer/ExplorerSidebar'
import { GraphOutline } from '@/features/graph/GraphOutline'
import { RunsSidebar } from '@/features/runs/RunsSidebar'
import { SettingsNav } from '@/features/settings/SettingsNav'
import { useUi, type ViewId } from '@/state/ui'

const TITLES: Record<ViewId, string> = {
  runs: 'Runs',
  graph: 'Node Map',
  agents: 'Agents',
  artifacts: 'Artifacts',
  explorer: 'Explorer',
  settings: 'Settings',
}

export function SideBar() {
  const activeView = useUi((s) => s.activeView)

  return (
    <div className="flex h-full min-h-0 flex-col">
      <SectionLabel className="shrink-0 border-b border-line-1">
        {TITLES[activeView]}
      </SectionLabel>

      <div className="min-h-0 flex-1">
        {activeView === 'runs' ? <RunsSidebar /> : null}
        {activeView === 'graph' ? <GraphOutline /> : null}
        {activeView === 'agents' ? <AgentsSidebar /> : null}
        {activeView === 'artifacts' ? <ArtifactsSidebar /> : null}
        {activeView === 'explorer' ? <ExplorerSidebar /> : null}
        {activeView === 'settings' ? <SettingsNav /> : null}
      </div>
    </div>
  )
}
