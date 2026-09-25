import { useEffect, useRef, useState } from 'react'
import { Terminal as XtermTerminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { RotateCcw, Square } from 'lucide-react'
import { IconButton } from '@/design/primitives'
import { bridge } from '@/state/bridge'
import { useActiveRun } from '@/state/store'

export function TerminalPanel() {
  const run = useActiveRun()
  const hostRef = useRef<HTMLDivElement>(null)
  const terminalRef = useRef<XtermTerminal | null>(null)
  const sessionRef = useRef<string | null>(null)
  const [cwd, setCwd] = useState('Resolving workspace…')
  const [generation, setGeneration] = useState(0)

  useEffect(() => {
    const host = hostRef.current
    if (!host || !bridge) return
    const terminalBridge = bridge.terminal
    const terminal = new XtermTerminal({
      cursorBlink: true,
      convertEol: true,
      fontFamily: '"JetBrains Mono Variable", monospace',
      fontSize: 12,
      lineHeight: 1.25,
      scrollback: 5000,
      theme: terminalTheme(),
    })
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.open(host)
    terminalRef.current = terminal

    let disposed = false
    let resizeTimer: ReturnType<typeof setTimeout> | undefined
    const observer = new ResizeObserver(() => {
      clearTimeout(resizeTimer)
      resizeTimer = setTimeout(() => {
        if (disposed) return
        fit.fit()
        const id = sessionRef.current
        if (id) void terminalBridge.resize(id, terminal.cols, terminal.rows)
      }, 40)
    })
    observer.observe(host)

    const offData = terminalBridge.onData(chunk => {
      if (chunk.id !== sessionRef.current) return
      terminal.write(chunk.data)
      const found = chunk.data.match(/https?:\/\/(?:localhost|127\.0\.0\.1)(?::\d+)?(?:\/[^\s\x1b]*)?/i)
      if (found) window.dispatchEvent(new CustomEvent('reticle-preview-url', { detail: found[0] }))
    })
    const offExit = terminalBridge.onExit(event => {
      if (event.id !== sessionRef.current) return
      terminal.write(`\r\n\x1b[90m[process exited ${event.exitCode}]\x1b[0m\r\n`)
      sessionRef.current = null
    })
    const input = terminal.onData(data => {
      const id = sessionRef.current
      if (id) void terminalBridge.write(id, data)
    })

    void (async () => {
      const summary = await bridge.workspace.summary(run?.execId)
      if (disposed) return
      if (!summary.ok || !summary.data) {
        setCwd('Workspace unavailable')
        terminal.writeln(`\x1b[31m${summary.error ?? 'Workspace unavailable'}\x1b[0m`)
        return
      }
      setCwd(summary.data.rootPath)
      fit.fit()
      const started = await terminalBridge.start({
        cwd: summary.data.rootPath,
        cols: terminal.cols,
        rows: terminal.rows,
      })
      if (disposed) {
        if (started.ok && started.data) void terminalBridge.stop(started.data.id)
        return
      }
      if (!started.ok || !started.data) {
        terminal.writeln(`\x1b[31mUnable to start terminal: ${started.error ?? 'unknown error'}\x1b[0m`)
        return
      }
      sessionRef.current = started.data.id
    })()

    return () => {
      disposed = true
      clearTimeout(resizeTimer)
      observer.disconnect()
      offData()
      offExit()
      input.dispose()
      if (sessionRef.current) void terminalBridge.stop(sessionRef.current)
      sessionRef.current = null
      terminal.dispose()
      terminalRef.current = null
    }
  }, [run?.execId, generation])

  return (
    <div className="flex h-full min-h-0 flex-col bg-inset">
      <div className="flex h-8 shrink-0 items-center gap-2 border-b border-line-1 px-2 text-2xs text-fg-4">
        <span className="mono min-w-0 flex-1 truncate" title={cwd}>{cwd}</span>
        <IconButton label="Restart terminal" size="sm" onClick={() => setGeneration(value => value + 1)}>
          <RotateCcw size={12} />
        </IconButton>
        <IconButton
          label="Stop terminal"
          size="sm"
          onClick={() => {
            const id = sessionRef.current
            if (id && bridge) void bridge.terminal.stop(id)
          }}
        >
          <Square size={11} />
        </IconButton>
      </div>
      <div ref={hostRef} className="min-h-0 flex-1 px-2 py-1" />
    </div>
  )
}

function terminalTheme() {
  const css = getComputedStyle(document.documentElement)
  const token = (name: string, fallback: string) => css.getPropertyValue(name).trim() || fallback
  return {
    background: token('--color-bg-inset', '#08090b'),
    foreground: token('--color-fg-2', '#d3d7df'),
    cursor: token('--color-accent', '#68a7ff'),
    selectionBackground: token('--color-accent-weak', '#29476e'),
    black: '#17191d', red: '#e06c75', green: '#98c379', yellow: '#e5c07b',
    blue: '#61afef', magenta: '#c678dd', cyan: '#56b6c2', white: '#d7dae0',
  }
}
