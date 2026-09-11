import { execFile } from 'node:child_process'
import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import fs from 'node:fs'
import { EventEmitter } from 'node:events'
import type {
  ForgeOutputChunk,
  ForgeStartRequest,
  ForgeState,
  StudioSettings,
} from '../../src/shared/ipc'

/**
 * Manages `forge.exe` as a child process.
 *
 * Two constraints are load-bearing and easy to get wrong:
 *
 *  1. **cwd must be `<repo>/cmd/forge`.** `cmd/forge/main.go:166` resolves its
 *     root as `filepath.Abs("../../")`, and the logger and waitlist reach for
 *     `../../logs` and `../../.reticle`. Launch it anywhere else and sessions,
 *     logs and artifacts silently resolve to the wrong directory.
 *
 *  2. **`-native` must always be passed.** Without it, `main.go:124-143` runs
 *     `docker info` and, on failure, prompts for y/n on stdin. A spawned child
 *     has no one to answer, so the process hangs forever with no output — which
 *     looks like a Studio bug rather than a blocked prompt.
 */
export class ForgeProcess extends EventEmitter {
  private child: ChildProcessWithoutNullStreams | null = null
  private state: ForgeState = { phase: 'stopped' }
  private stdoutTail = ''
  private stderrTail = ''

  getState(): ForgeState {
    return this.state
  }

  private setState(patch: Partial<ForgeState>): void {
    this.state = { ...this.state, ...patch }
    this.emit('state', this.state)
  }

  private emitLine(stream: ForgeOutputChunk['stream'], line: string): void {
    if (!line) return
    this.emit('output', { stream, line, at: Date.now() } satisfies ForgeOutputChunk)
  }

  /** Split a chunk into complete lines, carrying the partial tail forward. */
  private consume(stream: ForgeOutputChunk['stream'], chunk: string): void {
    const buffered = (stream === 'stdout' ? this.stdoutTail : this.stderrTail) + chunk
    const lines = buffered.split(/\r?\n/)
    const tail = lines.pop() ?? ''
    if (stream === 'stdout') this.stdoutTail = tail
    else this.stderrTail = tail
    for (const line of lines) this.emitLine(stream, line)
  }

  start(request: ForgeStartRequest, settings: StudioSettings): ForgeState {
    if (this.child) return this.state

    const binaryPath = settings.forge.binaryPath
    const cwd = settings.forge.cwd

    if (!binaryPath || !fs.existsSync(binaryPath)) {
      this.setState({
        phase: 'error',
        message: binaryPath
          ? `forge binary not found at ${binaryPath}. Build it with: cd cmd/forge && go build -o forge.exe`
          : 'No forge binary configured. Set it in Settings → Forge.',
      })
      return this.state
    }
    if (!cwd || !fs.existsSync(cwd)) {
      this.setState({
        phase: 'error',
        message: `forge working directory not found: ${cwd || '(unset)'}. It must be <repo>/cmd/forge.`,
      })
      return this.state
    }

    const args = buildArgs(request, settings)

    this.setState({
      phase: 'starting',
      binaryPath,
      cwd,
      args,
      startedAt: Date.now(),
      exitCode: undefined,
      message: undefined,
    })

    let child: ChildProcessWithoutNullStreams
    try {
      child = spawn(binaryPath, args, {
        cwd,
        windowsHide: true,
        env: { ...process.env },
      })
    } catch (error) {
      this.setState({
        phase: 'error',
        message: error instanceof Error ? error.message : String(error),
      })
      return this.state
    }

    this.child = child
    this.stdoutTail = ''
    this.stderrTail = ''
    this.setState({ phase: 'running', pid: child.pid })

    child.stdout.setEncoding('utf8')
    child.stdout.on('data', (chunk: string) => this.consume('stdout', chunk))
    child.stderr.setEncoding('utf8')
    child.stderr.on('data', (chunk: string) => this.consume('stderr', chunk))

    child.on('error', (error) => {
      if (this.child === child) this.child = null
      this.setState({ phase: 'error', message: error.message })
    })

    child.on('close', (code) => {
      if (this.child !== child) return
      // Flush whatever was left without a trailing newline.
      if (this.stdoutTail) this.emitLine('stdout', this.stdoutTail)
      if (this.stderrTail) this.emitLine('stderr', this.stderrTail)
      this.stdoutTail = ''
      this.stderrTail = ''
      this.child = null
      this.setState({ phase: 'exited', exitCode: code, pid: undefined })
    })

    return this.state
  }

  stop(): ForgeState {
    const child = this.child
    if (!child) return this.state

    // forge blocks on SIGINT when stdin reaches EOF (main.go:402-406), so ask
    // politely first and escalate only if it ignores us.
    try {
      if (process.platform === 'win32') {
        child.stdin.write('exit\n')
      } else {
        child.kill('SIGINT')
      }
    } catch {
      // Already gone.
    }

    const pid = child.pid
    setTimeout(() => {
      if (this.child && this.child.pid === pid) {
        try {
          if(process.platform==='win32' && pid) execFile('taskkill',['/PID',String(pid),'/T','/F'],{windowsHide:true},()=>{});else this.child.kill('SIGKILL')
        } catch {
          // Already gone.
        }
      }
    }, 4_000)

    return this.state
  }

  dispose(): void {
    if (this.child) {
      try {
        if(process.platform==='win32' && this.child.pid) execFile('taskkill',['/PID',String(this.child.pid),'/T','/F'],{windowsHide:true},()=>{});else this.child.kill()
      } catch {
        // Already gone.
      }
      this.child = null
    }
    this.removeAllListeners()
  }
}

function buildArgs(request: ForgeStartRequest, settings: StudioSettings): string[] {
  const args: string[] = [
    // Non-negotiable: without this forge may block on an interactive stdin prompt.

    `-port=${request.port}`,
    `-batch=${request.batch ?? settings.forge.batch}`,
    `-retries=${request.retries ?? settings.forge.retries}`,
    `-isolated=${request.isolated ?? settings.forge.isolated}`,
  ]
  if (request.native ?? settings.forge.native) args.push('-native')
  if (request.allModels ?? settings.forge.allModels) args.push('-all-models')
  if (request.fresh) args.push('-fresh')
  
  const workspace = request.workspace ?? settings.forge.workspace
  if (workspace) args.push(`-workspace=${workspace}`)

  // Positional args are joined into the initial prompt and auto-enqueued.
  const prompt = request.prompt?.trim()
  if (prompt) args.push(prompt)
  return args
}
