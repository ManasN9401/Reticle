import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { ExternalLink, Globe, RefreshCw } from 'lucide-react'
import { Button, EmptyState, IconButton, Spinner } from '@/design/primitives'
import { PREVIEW_URL_KEY } from '@/state/actions'
import { bridge } from '@/state/bridge'

const DEFAULT_URL = 'http://127.0.0.1:3000'

export function PreviewPanel() {
  const [input, setInput] = useState(() => localStorage.getItem(PREVIEW_URL_KEY) ?? DEFAULT_URL)
  const [url, setUrl] = useState(() => normalizeLocalUrl(input) ?? DEFAULT_URL)
  const [reload, setReload] = useState(0)
  const [error, setError] = useState<string | null>(null)
  // Whether anything answers at the address. Chromium's own "refused to connect"
  // page is always light, so an unreachable address gets a themed state instead.
  const probeKey = `${url}|${reload}`
  const [probe, setProbe] = useState<{ key: string; up: boolean } | null>(null)
  const reachable = probe?.key === probeKey ? probe.up : null

  useEffect(() => {
    let cancelled = false
    let timedOut = false
    const controller = new AbortController()
    const timer = setTimeout(() => {
      timedOut = true
      controller.abort()
    }, 4000)
    // no-cors: the page cannot be read, but a refused connection still rejects.
    // A server that is merely slow is given to the iframe rather than called down.
    fetch(url, { mode: 'no-cors', cache: 'no-store', signal: controller.signal })
      .then(() => {
        if (!cancelled) setProbe({ key: probeKey, up: true })
      })
      .catch(() => {
        if (!cancelled) setProbe({ key: probeKey, up: timedOut })
      })
      .finally(() => clearTimeout(timer))
    return () => {
      cancelled = true
      controller.abort()
      clearTimeout(timer)
    }
  }, [url, probeKey])

  useEffect(() => {
    const discovered = (event: Event) => {
      const next = normalizeLocalUrl((event as CustomEvent<string>).detail)
      if (!next) return
      setInput(next)
      setUrl(next)
      setError(null)
      localStorage.setItem(PREVIEW_URL_KEY, next)
    }
    window.addEventListener('reticle-preview-url', discovered)
    return () => window.removeEventListener('reticle-preview-url', discovered)
  }, [])

  const navigate = (event: FormEvent) => {
    event.preventDefault()
    const next = normalizeLocalUrl(input)
    if (!next) {
      setError('Preview accepts only http://localhost or http://127.0.0.1 addresses.')
      return
    }
    setError(null)
    setUrl(next)
    localStorage.setItem(PREVIEW_URL_KEY, next)
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-bg-0">
      <form onSubmit={navigate} className="flex h-9 shrink-0 items-center gap-1.5 border-b border-line-1 px-2">
        <IconButton label="Reload preview" size="sm" onClick={() => setReload(value => value + 1)}>
          <RefreshCw size={12} />
        </IconButton>
        <input
          aria-label="Preview address"
          value={input}
          onChange={event => setInput(event.target.value)}
          className="mono h-6 min-w-0 flex-1 rounded border border-line-2 bg-inset px-2 text-2xs text-fg-2 outline-none focus:border-accent"
          spellCheck={false}
        />
        <button type="submit" className="h-6 rounded bg-accent px-2 text-2xs text-white">Open</button>
        <IconButton label="Open in default browser" size="sm" onClick={() => void bridge?.workspace.openExternal(url)}>
          <ExternalLink size={12} />
        </IconButton>
      </form>
      {error ? <div className="border-b border-st-failed/30 bg-st-failed-weak px-3 py-1 text-2xs text-st-failed">{error}</div> : null}
      {reachable === null ? (
        <div className="flex min-h-0 flex-1 items-center justify-center gap-2 bg-inset text-xs text-fg-3">
          <Spinner /> Checking {url}…
        </div>
      ) : reachable === false ? (
        <div className="min-h-0 flex-1 bg-inset">
          <EmptyState
            icon={<Globe size={26} strokeWidth={1.4} />}
            title="Nothing is serving this address"
            description={
              <>
                No site answered at <span className="mono text-fg-2">{url}</span>. Start a dev server
                there, or open a finished run&apos;s preview from its notification.
              </>
            }
            action={
              <Button size="sm" onClick={() => setReload((value) => value + 1)}>
                Try again
              </Button>
            }
          />
        </div>
      ) : (
        <iframe
          key={`${url}:${reload}`}
          title="Local application preview"
          src={url}
          className="min-h-0 flex-1 border-0 bg-white"
          sandbox="allow-forms allow-modals allow-popups allow-same-origin allow-scripts"
          referrerPolicy="no-referrer"
        />
      )}
    </div>
  )
}

function normalizeLocalUrl(value: string): string | null {
  try {
    const candidate = /^https?:\/\//i.test(value.trim()) ? value.trim() : `http://${value.trim()}`
    const parsed = new URL(candidate)
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return null
    if (parsed.hostname !== 'localhost' && parsed.hostname !== '127.0.0.1' && parsed.hostname !== '[::1]') return null
    parsed.username = ''
    parsed.password = ''
    return parsed.toString()
  } catch {
    return null
  }
}
