import { EventEmitter } from 'node:events'
import WebSocket from 'ws'
import { decodeSocketEvent, type RuntimeEvent } from '../../src/shared/events'
import type { ConnectionState, OutboundCommand } from '../../src/shared/ipc'

const BACKOFF_BASE_MS = 500
const BACKOFF_MAX_MS = 15_000
const RATE_WINDOW_MS = 2_000

export interface ForgeClientEvents {
  event: (event: RuntimeEvent) => void
  state: (state: ConnectionState) => void
}

/**
 * WebSocket client for the orchestrator's telemetry stream.
 *
 * Lives in the main process so that run history survives renderer reloads and
 * so a single socket serves every window. Reconnects with exponential backoff:
 * losing forge mid-run is routine, and the UI must recover without the user
 * doing anything.
 */
export class ForgeClient extends EventEmitter {
  private socket: WebSocket | null = null
  private reconnectTimer: NodeJS.Timeout | null = null
  private rateTimer: NodeJS.Timeout | null = null
  private eventsInWindow = 0
  /** Set when the user explicitly disconnects, to suppress auto-reconnect. */
  private intentionallyClosed = false

  private state: ConnectionState = {
    phase: 'idle',
    host: '127.0.0.1',
    port: 8080,
    attempt: 0,
    eventRate: 0,
  }

  getState(): ConnectionState {
    return this.state
  }

  private setState(patch: Partial<ConnectionState>): void {
    this.state = { ...this.state, ...patch }
    this.emit('state', this.state)
  }

  connect(host: string, port: number): ConnectionState {
    this.intentionallyClosed = false
    this.clearReconnect()

    const changedTarget = host !== this.state.host || port !== this.state.port
    if (changedTarget) this.teardownSocket()

    if (this.socket && this.socket.readyState === WebSocket.OPEN) {
      return this.state
    }

    this.setState({
      host,
      port,
      phase: this.state.attempt > 0 ? 'reconnecting' : 'connecting',
      message: undefined,
    })
    this.open()
    return this.state
  }

  disconnect(): ConnectionState {
    this.intentionallyClosed = true
    this.clearReconnect()
    this.teardownSocket()
    this.stopRateSampling()
    this.setState({ phase: 'idle', attempt: 0, eventRate: 0, connectedAt: undefined })
    return this.state
  }

  /**
   * The runtime whitelists inbound actions to `enqueue` and `remove`
   * (runtime/telemetry/server.go:250); anything else is accepted by the socket
   * and then silently dropped, so `OutboundCommand` refuses to express it.
   */
  send(command: OutboundCommand): boolean {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) return false
    try {
      this.socket.send(JSON.stringify(command))
      return true
    } catch (error) {
      console.error('[forge-client] send failed', error)
      return false
    }
  }

  private open(): void {
    const { host, port } = this.state
    const url = `ws://${host}:${port}/ws`

    let socket: WebSocket
    try {
      socket = new WebSocket(url, { handshakeTimeout: 8_000 })
    } catch (error) {
      this.onFailure(error instanceof Error ? error.message : String(error))
      return
    }
    this.socket = socket

    socket.on('open', () => {
      this.setState({
        phase: 'connected',
        attempt: 0,
        connectedAt: Date.now(),
        message: undefined,
      })
      this.startRateSampling()
    })

    socket.on('message', (data) => {
      // The server replays WaitlistUpdated + WorkflowStarted on connect, so the
      // first frames after `open` are a partial state snapshot.
      const event = decodeSocketEvent(
        typeof data === 'string' ? data : data.toString('utf8'),
      )
      if (!event) return
      this.eventsInWindow += 1
      this.emit('event', event)
    })

    socket.on('error', (error: Error) => {
      // 'close' always follows; record the reason for the status bar.
      this.setState({ message: error.message })
    })

    socket.on('close', () => {
      this.socket = null
      this.stopRateSampling()
      if (this.intentionallyClosed) return
      this.onFailure(this.state.message)
    })
  }

  private onFailure(message: string | undefined): void {
    const attempt = this.state.attempt + 1
    const delay = Math.min(BACKOFF_BASE_MS * 2 ** (attempt - 1), BACKOFF_MAX_MS)
    this.setState({
      phase: 'reconnecting',
      attempt,
      message,
      eventRate: 0,
      connectedAt: undefined,
    })
    this.clearReconnect()
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      if (!this.intentionallyClosed) this.open()
    }, delay)
  }

  private startRateSampling(): void {
    this.stopRateSampling()
    this.eventsInWindow = 0
    this.rateTimer = setInterval(() => {
      const rate = Math.round((this.eventsInWindow / RATE_WINDOW_MS) * 1000)
      this.eventsInWindow = 0
      if (rate !== this.state.eventRate) this.setState({ eventRate: rate })
    }, RATE_WINDOW_MS)
  }

  private stopRateSampling(): void {
    if (this.rateTimer) {
      clearInterval(this.rateTimer)
      this.rateTimer = null
    }
  }

  private clearReconnect(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
  }

  private teardownSocket(): void {
    if (!this.socket) return
    this.socket.removeAllListeners()
    try {
      this.socket.close()
    } catch {
      // Already closing; nothing useful to do.
    }
    this.socket = null
  }

  dispose(): void {
    this.intentionallyClosed = true
    this.clearReconnect()
    this.stopRateSampling()
    this.teardownSocket()
    this.removeAllListeners()
  }
}
