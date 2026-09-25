import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { ExternalLink, RefreshCw } from 'lucide-react'
import { IconButton } from '@/design/primitives'
import { bridge } from '@/state/bridge'

const DEFAULT_URL = 'http://127.0.0.1:3000'

export function PreviewPanel() {
  const [input, setInput] = useState(() => localStorage.getItem('reticle.previewUrl') ?? DEFAULT_URL)
  const [url, setUrl] = useState(() => normalizeLocalUrl(input) ?? DEFAULT_URL)
  const [reload, setReload] = useState(0)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const discovered = (event: Event) => {
      const next = normalizeLocalUrl((event as CustomEvent<string>).detail)
      if (!next) return
      setInput(next)
      setUrl(next)
      setError(null)
      localStorage.setItem('reticle.previewUrl', next)
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
    localStorage.setItem('reticle.previewUrl', next)
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
      <iframe
        key={`${url}:${reload}`}
        title="Local application preview"
        src={url}
        className="min-h-0 flex-1 border-0 bg-white"
        sandbox="allow-forms allow-modals allow-popups allow-same-origin allow-scripts"
        referrerPolicy="no-referrer"
      />
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
