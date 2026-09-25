import fs from 'node:fs'
import path from 'node:path'
import { randomUUID } from 'node:crypto'
import * as pty from 'node-pty'
import type { ApiResult, TerminalExit, TerminalSession } from '../../src/shared/ipc'
import { isInside } from '../paths'

/** Owns pseudo-terminals in the trusted main process; the renderer sees only text. */
export class TerminalManager {
  private sessions = new Map<string, pty.IPty>()
  private root: () => string | null
  private onData: (id: string, data: string) => void
  private onExit: (event: TerminalExit) => void

  constructor(
    root: () => string | null,
    onData: (id: string, data: string) => void,
    onExit: (event: TerminalExit) => void,
  ) {
    this.root = root
    this.onData = onData
    this.onExit = onExit
  }

  start(cwd: string, cols: number, rows: number): ApiResult<TerminalSession> {
    const root = this.root()
    if (!root || !fs.existsSync(cwd) || !fs.statSync(cwd).isDirectory() || !isInside(root, cwd)) {
      return { ok: false, error: 'Terminal directory must be inside the configured Reticle checkout.' }
    }

    const shell = process.platform === 'win32'
      ? (process.env.COMSPEC || 'powershell.exe')
      : (process.env.SHELL || '/bin/bash')
    const args = process.platform === 'win32' && path.basename(shell).toLowerCase() === 'powershell.exe'
      ? ['-NoLogo']
      : []
    const id = randomUUID()
    try {
      const child = pty.spawn(shell, args, {
        name: 'xterm-256color',
        cols: clamp(cols, 20, 500),
        rows: clamp(rows, 5, 200),
        cwd,
        env: { ...process.env, TERM: 'xterm-256color' } as Record<string, string>,
      })
      this.sessions.set(id, child)
      child.onData(data => this.onData(id, data))
      child.onExit(({ exitCode, signal }) => {
        this.sessions.delete(id)
        this.onExit({ id, exitCode, signal })
      })
      return { ok: true, data: { id, cwd, shell: path.basename(shell) } }
    } catch (error) {
      return { ok: false, error: error instanceof Error ? error.message : String(error) }
    }
  }

  write(id: string, data: string): boolean {
    const session = this.sessions.get(id)
    if (!session || typeof data !== 'string' || data.length > 64 * 1024) return false
    session.write(data)
    return true
  }

  resize(id: string, cols: number, rows: number): boolean {
    const session = this.sessions.get(id)
    if (!session) return false
    session.resize(clamp(cols, 20, 500), clamp(rows, 5, 200))
    return true
  }

  stop(id: string): void {
    const session = this.sessions.get(id)
    if (!session) return
    this.sessions.delete(id)
    session.kill()
  }

  dispose(): void {
    for (const session of this.sessions.values()) session.kill()
    this.sessions.clear()
  }
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, Math.round(Number.isFinite(value) ? value : min)))
}
