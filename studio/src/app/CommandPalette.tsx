import { useEffect, useMemo, useRef, useState } from 'react'
import { cn } from '@/design/cn'
import { Kbd } from '@/design/primitives'
import { useUi } from '@/state/ui'
import { COMMANDS, isEnabled, type Command } from './commands'

/**
 * Command palette. Resolves through the same registry as the native menu and
 * the keybindings, so anything reachable here is reachable everywhere.
 */
export function CommandPalette() {
  const open = useUi((s) => s.paletteOpen)
  return open ? <PaletteContents /> : null
}

function PaletteContents() {
  const setPalette = useUi((s) => s.setPalette)
  const [query, setQuery] = useState('')
  const [index, setIndex] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const results = useMemo(() => {
    const needle = query.trim().toLowerCase()
    const commands = needle
      ? COMMANDS.filter(
          (command) =>
            command.title.toLowerCase().includes(needle) ||
            command.section.toLowerCase().includes(needle),
        )
      : COMMANDS
    // Enabled commands first — a disabled entry should never be the default target.
    return [...commands].sort(
      (a, b) => Number(isEnabled(b)) - Number(isEnabled(a)),
    )
  }, [query])

  useEffect(() => {
    // Focus after paint so the overlay is mounted first.
    requestAnimationFrame(() => inputRef.current?.focus())
  }, [])

  useEffect(() => {
    listRef.current
      ?.querySelector<HTMLElement>(`[data-index="${index}"]`)
      ?.scrollIntoView({ block: 'nearest' })
  }, [index])

  const commit = (command: Command | undefined) => {
    if (!command || !isEnabled(command)) return
    setPalette(false)
    void command.run()
  }

  return (
    <div
      className="fixed inset-0 z-[100] flex items-start justify-center bg-black/45 pt-[12vh]"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) setPalette(false)
      }}
    >
      <div
        role="dialog"
        aria-label="Command palette"
        className="w-[min(620px,calc(100vw-64px))] overflow-hidden rounded-[var(--radius-card)] border border-line-2 bg-bg-2 shadow-[var(--shadow-modal)]"
      >
        <input
          ref={inputRef}
          value={query}
          onChange={(event) => { setQuery(event.target.value); setIndex(0) }}
          placeholder="Type a command…"
          aria-label="Command"
          className="h-11 w-full border-b border-line-1 bg-transparent px-4 text-base text-fg-1 outline-none placeholder:text-fg-4"
          onKeyDown={(event) => {
            if (event.key === 'Escape') {
              event.preventDefault()
              setPalette(false)
            } else if (event.key === 'ArrowDown') {
              event.preventDefault()
              setIndex((i) => Math.min(results.length - 1, i + 1))
            } else if (event.key === 'ArrowUp') {
              event.preventDefault()
              setIndex((i) => Math.max(0, i - 1))
            } else if (event.key === 'Enter') {
              event.preventDefault()
              commit(results[index])
            }
          }}
        />

        <div ref={listRef} className="max-h-[46vh] overflow-y-auto py-1">
          {results.length === 0 ? (
            <div className="px-4 py-6 text-center text-xs text-fg-4">
              No matching commands
            </div>
          ) : (
            results.map((command, i) => {
              const enabled = isEnabled(command)
              return (
                <button
                  key={command.id}
                  type="button"
                  data-index={i}
                  disabled={!enabled}
                  onMouseMove={() => setIndex(i)}
                  onClick={() => commit(command)}
                  title={!enabled ? command.disabledReason : undefined}
                  className={cn(
                    'flex w-full items-center gap-3 px-4 py-1.5 text-left',
                    '[transition-property:background-color] duration-[var(--dur-fast)]',
                    i === index && enabled ? 'bg-accent-weak' : 'bg-transparent',
                    enabled ? 'text-fg-1' : 'cursor-default text-fg-4',
                  )}
                >
                  <span className="w-14 shrink-0 text-2xs tracking-wide text-fg-4 uppercase">
                    {command.section}
                  </span>
                  <span className="min-w-0 flex-1 truncate-1 text-sm">
                    {command.title}
                  </span>
                  {!enabled && command.disabledReason ? (
                    <span className="truncate-1 max-w-[24ch] text-2xs text-fg-4">
                      {command.disabledReason}
                    </span>
                  ) : null}
                  {command.keys ? <Kbd>{command.keys}</Kbd> : null}
                </button>
              )
            })
          )}
        </div>
      </div>
    </div>
  )
}
