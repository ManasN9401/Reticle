import { cn } from '@/design/cn'
import { SETTINGS_SECTIONS, useSettingsUi } from './state'

export function SettingsNav() {
  const section = useSettingsUi((s) => s.section)
  const setSection = useSettingsUi((s) => s.setSection)

  return (
    <nav className="flex h-full min-h-0 flex-col overflow-y-auto py-1">
      {SETTINGS_SECTIONS.map((item) => (
        <button
          key={item.id}
          type="button"
          onClick={() => setSection(item.id)}
          aria-current={section === item.id ? 'true' : undefined}
          className={cn(
            'flex flex-col items-start gap-0.5 px-3 py-1.5 text-left',
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
    </nav>
  )
}
