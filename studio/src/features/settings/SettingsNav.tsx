import { useMemo, useState } from 'react'
import { Search } from 'lucide-react'
import { cn } from '@/design/cn'
import { Input } from '@/design/primitives'
import { SETTINGS_GROUPS, SETTINGS_SECTIONS, useSettingsUi } from './state'

export function SettingsNav() {
  const section = useSettingsUi((s) => s.section)
  const setSection = useSettingsUi((s) => s.setSection)
  const [filter, setFilter] = useState('')

  const groups = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    return SETTINGS_GROUPS.map((group) => ({
      group,
      items: SETTINGS_SECTIONS.filter(
        (item) =>
          item.group === group.id &&
          (!needle ||
            item.label.toLowerCase().includes(needle) ||
            item.hint.toLowerCase().includes(needle)),
      ),
    })).filter((g) => g.items.length > 0)
  }, [filter])

  return (
    <nav className="flex h-full min-h-0 flex-col">
      <div className="relative shrink-0 px-2 pt-1.5 pb-1">
        <Search
          size={12}
          strokeWidth={1.8}
          className="pointer-events-none absolute top-1/2 left-4 -translate-y-1/2 text-fg-4"
        />
        <Input
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
          placeholder="Filter settings"
          aria-label="Filter settings"
          className="h-6 w-full pl-6 text-xs"
        />
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto py-1">
        {groups.map(({ group, items }) => (
          <div key={group.id} className="mb-1 last:mb-0">
            <div className="px-3 pt-2 pb-0.5 text-2xs font-semibold tracking-[0.08em] text-fg-4 uppercase">
              {group.label}
            </div>
            {items.map((item) => (
              <button
                key={item.id}
                type="button"
                onClick={() => setSection(item.id)}
                aria-current={section === item.id ? 'true' : undefined}
                className={cn(
                  'flex w-full flex-col items-start gap-0.5 px-3 py-1.5 text-left',
                  '[transition-property:background-color] duration-[var(--dur-fast)]',
                  section === item.id ? 'bg-accent-weak' : 'hover:bg-bg-2',
                )}
              >
                <span
                  className={cn(
                    'text-xs',
                    section === item.id ? 'text-fg-1' : 'text-fg-2',
                  )}
                >
                  {item.label}
                </span>
                <span className="text-2xs text-fg-4">{item.hint}</span>
              </button>
            ))}
          </div>
        ))}
        {groups.length === 0 ? (
          <div className="px-3 py-3 text-2xs text-fg-4">No settings match “{filter}”.</div>
        ) : null}
      </div>
    </nav>
  )
}
