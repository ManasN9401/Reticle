import fs from 'node:fs'
import http from 'node:http'
import type { AddressInfo } from 'node:net'
import path from 'node:path'

/**
 * Serves one run's generated site (<session>/src) to the Preview panel.
 *
 * The files were written by an LLM-driven worker, so the server is deliberately
 * narrow: loopback only, GET/HEAD only, no directory listings, no dotfiles
 * (.env, .rag, .git), a Host check against DNS rebinding, and every request is
 * re-checked against the real (symlink-resolved) path of the run's root.
 * Electron-free so it can be exercised with plain node:test.
 */

const EXEC_ID = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/

const CONTENT_TYPES: Record<string, string> = {
  '.html': 'text/html; charset=utf-8',
  '.htm': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.map': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.gif': 'image/gif',
  '.webp': 'image/webp',
  '.avif': 'image/avif',
  '.ico': 'image/x-icon',
  '.txt': 'text/plain; charset=utf-8',
  '.md': 'text/plain; charset=utf-8',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.ttf': 'font/ttf',
  '.mp3': 'audio/mpeg',
  '.wav': 'audio/wav',
  '.mp4': 'video/mp4',
  '.webm': 'video/webm',
  '.wasm': 'application/wasm',
}

export interface PreviewServer {
  /** URL of a page in the run's site, or null when the run has no src folder. */
  urlFor(execId: string, entry?: string): Promise<string | null>
  close(): Promise<void>
}

function badSegment(segment: string): boolean {
  return (
    segment === '' ||
    segment === '.' ||
    segment === '..' ||
    segment.startsWith('.') ||
    /[\\/:\0]/.test(segment)
  )
}

function within(root: string, target: string): boolean {
  const relative = path.relative(root, target)
  return relative === '' || (!relative.startsWith('..') && !path.isAbsolute(relative))
}

function errorPage(status: number, message: string): string {
  return (
    '<!doctype html><meta charset="utf-8"><meta name="color-scheme" content="dark light">' +
    `<title>${status}</title>` +
    '<body style="font:14px system-ui,sans-serif;margin:0;padding:32px">' +
    `<h1 style="font-size:16px;margin:0 0 8px">${status}</h1>` +
    `<p style="margin:0;opacity:.7">${message}</p>`
  )
}

export function createPreviewServer(rootFor: (execId: string) => string | null): PreviewServer {
  let server: http.Server | null = null
  let starting: Promise<number> | null = null
  let port = 0

  const respond = (res: http.ServerResponse, status: number, message: string, headers: http.OutgoingHttpHeaders = {}) => {
    // Errors are shown inside the Preview iframe. A bare text/plain page there is
    // always light; declaring both schemes lets it follow the app theme.
    const page = status >= 400
    res.writeHead(status, {
      'Content-Type': page ? 'text/html; charset=utf-8' : 'text/plain; charset=utf-8',
      'X-Content-Type-Options': 'nosniff',
      'Cache-Control': 'no-store',
      ...headers,
    })
    res.end(page ? errorPage(status, message) : message)
  }

  async function handle(req: http.IncomingMessage, res: http.ServerResponse): Promise<void> {
    if (req.method !== 'GET' && req.method !== 'HEAD') {
      return respond(res, 405, 'Method not allowed', { Allow: 'GET, HEAD' })
    }
    const host = req.headers.host ?? ''
    if (![`127.0.0.1:${port}`, `localhost:${port}`, `[::1]:${port}`].includes(host)) {
      return respond(res, 403, 'Forbidden host')
    }

    // Not `new URL(req.url)`: a request target like //host/x would be read as a host.
    const target0 = req.url ?? '/'
    if (!target0.startsWith('/')) return respond(res, 400, 'Bad request')
    const queryAt = target0.indexOf('?')
    const pathname = queryAt < 0 ? target0 : target0.slice(0, queryAt)
    const search = queryAt < 0 ? '' : target0.slice(queryAt)
    let segments: string[]
    try {
      segments = pathname.split('/').slice(1).map((segment) => decodeURIComponent(segment))
    } catch {
      return respond(res, 400, 'Bad request')
    }
    const trailingSlash = segments[segments.length - 1] === ''
    if (trailingSlash) segments.pop()
    const [execId, ...rest] = segments
    if (!execId || !EXEC_ID.test(execId) || rest.some(badSegment)) return respond(res, 404, 'Not found')

    const base = rootFor(execId)
    if (!base) return respond(res, 404, 'Not found')
    let root: string
    let target: string
    try {
      root = await fs.promises.realpath(base)
      target = await fs.promises.realpath(path.join(root, ...rest))
    } catch {
      return respond(res, 404, 'Not found')
    }
    if (!within(root, target)) return respond(res, 404, 'Not found')

    let stat: fs.Stats
    try {
      stat = await fs.promises.stat(target)
    } catch {
      return respond(res, 404, 'Not found')
    }
    if (stat.isDirectory()) {
      // Relative links in a page only resolve against a directory URL ending in /.
      if (!trailingSlash && rest.length > 0) {
        return respond(res, 302, '', { Location: `${pathname}/${search}` })
      }
      try {
        target = await fs.promises.realpath(path.join(target, 'index.html'))
        stat = await fs.promises.stat(target)
      } catch {
        return respond(res, 404, 'Not found')
      }
      if (!within(root, target)) return respond(res, 404, 'Not found')
    }
    if (!stat.isFile()) return respond(res, 404, 'Not found')

    res.writeHead(200, {
      'Content-Type': CONTENT_TYPES[path.extname(target).toLowerCase()] ?? 'application/octet-stream',
      'Content-Length': stat.size,
      'X-Content-Type-Options': 'nosniff',
      'Cache-Control': 'no-store',
    })
    if (req.method === 'HEAD') return void res.end()
    const stream = fs.createReadStream(target)
    stream.on('error', () => res.destroy())
    stream.pipe(res)
  }

  function start(): Promise<number> {
    starting ??= new Promise<number>((resolve, reject) => {
      const created = http.createServer((req, res) => {
        handle(req, res).catch(() => {
          if (!res.headersSent) respond(res, 500, 'Internal error')
          else res.destroy()
        })
      })
      created.once('error', reject)
      created.listen(0, '127.0.0.1', () => {
        server = created
        port = (created.address() as AddressInfo).port
        resolve(port)
      })
    })
    return starting
  }

  return {
    async urlFor(execId, entry) {
      if (!EXEC_ID.test(execId)) return null
      const root = rootFor(execId)
      if (!root) return null
      try {
        if (!(await fs.promises.stat(root)).isDirectory()) return null
      } catch {
        return null
      }
      const entrySegments = (entry ?? '').split('/').filter((segment) => segment !== '')
      if (entrySegments.some(badSegment)) return null
      const listening = await start()
      const suffix = entrySegments.map(encodeURIComponent).join('/')
      return `http://127.0.0.1:${listening}/${encodeURIComponent(execId)}/${suffix}`
    },
    async close() {
      const active = server
      server = null
      starting = null
      if (!active) return
      active.closeAllConnections()
      await new Promise<void>((resolve) => active.close(() => resolve()))
    },
  }
}
