import { useCallback, useEffect, useRef, useState } from 'react'
import { cn } from '@/design/cn'
import { bridge } from '@/state/bridge'
import { useStudio } from '@/state/store'
import { COMMANDS, isEnabled, type Command } from './commands'
import { MENUS, parseMnemonic, type MenuDefinition, type MenuItem } from './menuModel'

const BY_ID = new Map(COMMANDS.map((command) => [command.id, command]))

/**
 * In-window menu bar.
 *
 * Exists because a frameless window renders no native menu bar on Windows or
 * Linux — the native menu is installed only on macOS, where it lives at the top
 * of the screen instead. Everything here resolves through the same command
 * registry as the palette and the keybindings, so a menu entry can never drift
 * out of sync with its shortcut.
 */
export function MenuBar() {
  const platform = useStudio((s) => s.windowState.platform)
  const [openIndex, setOpenIndex] = useState<number | null>(null)
  const rootRef = useRef<HTMLDivElement>(null)

  const close = useCallback(() => setOpenIndex(null), [])

  // Click-away and Escape both dismiss. Pointerdown rather than click so the
  // menu closes before the underlying control receives the press.
  useEffect(() => {
    if (openIndex === null) return
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) close()
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation()
        close()
      }
    }
    document.addEventListener('pointerdown', onPointerDown, true)
    document.addEventListener('keydown', onKeyDown, true)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown, true)
      document.removeEventListener('keydown', onKeyDown, true)
    }
  }, [openIndex, close])

  // Alt+<mnemonic> opens a menu, matching the platform convention.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!event.altKey || event.ctrlKey || event.metaKey) return
      const key = event.key.toLowerCase()
      const index = MENUS.findIndex((menu) => parseMnemonic(menu.label).key === key)
      if (index === -1) return
      event.preventDefault()
      setOpenIndex((current) => (current === index ? null : index))
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  // macOS already has a real menu bar above the window; drawing a second one
  // inside the title bar would be wrong there.
  if (platform === 'darwin') return null

  return (
    <div ref={rootRef} className="no-drag relative flex h-full items-stretch">
      {MENUS.map((menu, index) => (
        <MenuButton
          key={menu.label}
          menu={menu}
          open={openIndex === index}
          // Once one menu is open, hovering another switches to it — the
          // behaviour every desktop menu bar has.
          onHover={() => setOpenIndex((current) => (current === null ? null : index))}
          onToggle={() => setOpenIndex((current) => (current === index ? null : index))}
          onClose={close}
        />
      ))}
    </div>
  )
}

function MenuButton({
  menu,
  open,
  onHover,
  onToggle,
  onClose,
}: {
  menu: MenuDefinition
  open: boolean
  onHover: () => void
  onToggle: () => void
  onClose: () => void
}) {
  const { text, key } = parseMnemonic(menu.label)
  const mnemonicAt = key ? text.toLowerCase().indexOf(key) : -1

  return (
    <div className="relative flex items-stretch">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={onToggle}
        onPointerEnter={onHover}
        className={cn(
          'flex items-center px-2 text-xs whitespace-nowrap',
          '[transition-property:background-color,color] duration-[var(--dur-fast)] ease-[var(--ease-out-quint)]',
          'focus-visible:outline-2 focus-visible:outline-accent focus-visible:-outline-offset-2',
          open ? 'bg-bg-3 text-fg-1' : 'text-fg-2 hover:bg-bg-2 hover:text-fg-1',
        )}
      >
        {mnemonicAt === -1 ? (
          text
        ) : (
          <>
            {text.slice(0, mnemonicAt)}
            <span className="underline decoration-fg-4 underline-offset-2">
              {text[mnemonicAt]}
            </span>
            {text.slice(mnemonicAt + 1)}
          </>
        )}
      </button>

      {open ? <MenuDropdown items={menu.items} onClose={onClose} /> : null}
    </div>
  )
}

function MenuDropdown({ items, onClose }: { items: MenuItem[]; onClose: () => void }) {
  return (
    <div
      role="menu"
      className="absolute top-full left-0 z-[90] min-w-[240px] rounded-b-[var(--radius-card)] border border-line-2 bg-bg-2 py-1 shadow-[var(--shadow-menu)]"
    >
      {items.map((item, index) => {
        if (item.kind === 'separator') {
          return <div key={`sep-${index}`} className="my-1 h-px bg-line-1" />
        }

        if (item.kind === 'unavailable') {
          return (
            <Row
              key={item.label}
              label={item.label}
              disabled
              title={item.reason}
              hint="unavailable"
            />
          )
        }

        if (item.kind === 'native') {
          return (
            <Row
              key={item.label}
              label={item.label}
              keys={item.keys}
              onSelect={() => {
                onClose()
                void bridge?.native(item.action)
              }}
            />
          )
        }

        const command: Command | undefined = BY_ID.get(item.id)
        if (!command) return null
        const enabled = isEnabled(command)
        return (
          <Row
            key={item.id}
            label={item.label ?? command.title}
            keys={command.keys}
            disabled={!enabled}
            title={!enabled ? command.disabledReason : undefined}
            onSelect={() => {
              onClose()
              void command.run()
            }}
          />
        )
      })}
    </div>
  )
}

function Row({
  label,
  keys,
  hint,
  disabled,
  title,
  onSelect,
}: {
  label: string
  keys?: string
  hint?: string
  disabled?: boolean
  title?: string
  onSelect?: () => void
}) {
  return (
    <button
      type="button"
      role="menuitem"
      disabled={disabled}
      title={title}
      onClick={onSelect}
      className={cn(
        'flex w-full items-center gap-6 px-3 py-1 text-left text-xs whitespace-nowrap',
        '[transition-property:background-color,color] duration-[var(--dur-fast)]',
        disabled
          ? 'cursor-default text-fg-4'
          : 'text-fg-1 hover:bg-accent-weak focus-visible:bg-accent-weak focus-visible:outline-none',
      )}
    >
      <span className="flex-1">{label}</span>
      {keys ? <span className="mono text-2xs text-fg-4">{keys}</span> : null}
      {hint ? <span className="text-2xs text-fg-4 italic">{hint}</span> : null}
    </button>
  )
}
