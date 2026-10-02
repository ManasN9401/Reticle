import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import http from 'node:http'
import os from 'node:os'
import path from 'node:path'
import { createPreviewServer } from '../electron/preview/server'
import type { PreviewServer } from '../electron/preview/server'

interface Fixture {
  server: PreviewServer
  base: string
  sessions: string
  cleanup(): Promise<void>
}

async function fixture(): Promise<Fixture> {
  const sessions = fs.mkdtempSync(path.join(os.tmpdir(), 'reticle-preview-'))
  const src = path.join(sessions, 'exec-a', 'src')
  fs.mkdirSync(path.join(src, 'images'), { recursive: true })
  fs.mkdirSync(path.join(src, 'sub'), { recursive: true })
  fs.writeFileSync(path.join(src, 'index.html'), '<h1>home</h1>')
  fs.writeFileSync(path.join(src, 'style.css'), 'body{}')
  fs.writeFileSync(path.join(src, 'images', 'a.png'), Buffer.from([137, 80, 78, 71]))
  fs.writeFileSync(path.join(src, 'sub', 'index.html'), '<h1>sub</h1>')
  fs.writeFileSync(path.join(src, '.env'), 'SECRET=1')
  fs.writeFileSync(path.join(sessions, 'exec-a', 'outside.txt'), 'not in src')
  const server = createPreviewServer((execId) => (execId === 'exec-a' ? src : null))
  const base = (await server.urlFor('exec-a')) as string
  return {
    server,
    base: base.replace(/\/exec-a\/$/, ''),
    sessions,
    async cleanup() {
      await server.close()
      fs.rmSync(sessions, { recursive: true, force: true })
    },
  }
}

/** Raw request, because fetch normalizes ../ away before sending. */
function raw(base: string, requestPath: string, options: http.RequestOptions = {}): Promise<{ status: number; headers: http.IncomingHttpHeaders; body: string }> {
  return new Promise((resolve, reject) => {
    const url = new URL(base)
    const request = http.request({ host: url.hostname, port: url.port, path: requestPath, ...options }, (response) => {
      const chunks: Buffer[] = []
      response.on('data', (chunk) => chunks.push(chunk))
      response.on('end', () => resolve({ status: response.statusCode ?? 0, headers: response.headers, body: Buffer.concat(chunks).toString() }))
    })
    request.on('error', reject)
    request.end()
  })
}

test('serves the run site with types, no-sniff and no caching', async () => {
  const f = await fixture()
  try {
    const page = await raw(f.base, '/exec-a/')
    assert.equal(page.status, 200)
    assert.equal(page.body, '<h1>home</h1>')
    assert.equal(page.headers['content-type'], 'text/html; charset=utf-8')
    assert.equal(page.headers['x-content-type-options'], 'nosniff')
    assert.equal(page.headers['cache-control'], 'no-store')
    assert.equal((await raw(f.base, '/exec-a/style.css')).headers['content-type'], 'text/css; charset=utf-8')
    assert.equal((await raw(f.base, '/exec-a/images/a.png')).headers['content-type'], 'image/png')
  } finally {
    await f.cleanup()
  }
})

test('directories redirect to a trailing slash and serve their index, never a listing', async () => {
  const f = await fixture()
  try {
    const redirect = await raw(f.base, '/exec-a/sub?x=1')
    assert.equal(redirect.status, 302)
    assert.equal(redirect.headers.location, '/exec-a/sub/?x=1')
    assert.equal((await raw(f.base, '/exec-a/sub/')).body, '<h1>sub</h1>')
    assert.equal((await raw(f.base, '/exec-a/images/')).status, 404)
  } finally {
    await f.cleanup()
  }
})

test('traversal, encoded traversal, dotfiles and other runs are refused', async () => {
  const f = await fixture()
  try {
    for (const bad of [
      '/exec-a/../exec-a/outside.txt',
      '/exec-a/%2e%2e/outside.txt',
      '/exec-a/images/..%2f..%2foutside.txt',
      '/exec-a/images/%2e%2e/%2e%2e/outside.txt',
      '/exec-a/.env',
      '/exec-a/%2eenv',
      '/exec-a/sub/..\\..\\outside.txt',
      '/exec-b/index.html',
      '/..%2fexec-a/index.html',
      '/exec-a/%00',
      '//evil.test/exec-a/',
    ]) {
      const result = await raw(f.base, bad)
      assert.ok([400, 404].includes(result.status), `${bad} -> ${result.status}`)
      assert.ok(!result.body.includes('not in src') && !result.body.includes('SECRET'), bad)
    }
  } finally {
    await f.cleanup()
  }
})

test('only GET and HEAD are allowed, and HEAD sends no body', async () => {
  const f = await fixture()
  try {
    const post = await raw(f.base, '/exec-a/', { method: 'POST' })
    assert.equal(post.status, 405)
    const head = await raw(f.base, '/exec-a/', { method: 'HEAD' })
    assert.equal(head.status, 200)
    assert.equal(head.headers['content-length'], '13')
    assert.equal(head.body, '')
  } finally {
    await f.cleanup()
  }
})

test('a foreign Host header is rejected', async () => {
  const f = await fixture()
  try {
    assert.equal((await raw(f.base, '/exec-a/', { headers: { Host: 'attacker.test' } })).status, 403)
  } finally {
    await f.cleanup()
  }
})

test('a symlink inside src cannot reach files outside it', async (t) => {
  const f = await fixture()
  try {
    const link = path.join(f.sessions, 'exec-a', 'src', 'escape.txt')
    try {
      fs.symlinkSync(path.join(f.sessions, 'exec-a', 'outside.txt'), link)
    } catch {
      return t.skip('symlinks need privileges on this machine')
    }
    const result = await raw(f.base, '/exec-a/escape.txt')
    assert.equal(result.status, 404)
    assert.ok(!result.body.includes('not in src'))
  } finally {
    await f.cleanup()
  }
})

test('urlFor encodes the entry and refuses unknown runs and unsafe entries', async () => {
  const f = await fixture()
  try {
    const url = await f.server.urlFor('exec-a', 'sub/index.html')
    assert.match(url as string, /^http:\/\/127\.0\.0\.1:\d+\/exec-a\/sub\/index\.html$/)
    assert.match((await f.server.urlFor('exec-a', 'my page.html')) as string, /\/exec-a\/my%20page\.html$/)
    assert.equal(await f.server.urlFor('exec-b'), null)
    assert.equal(await f.server.urlFor('../exec-a'), null)
    assert.equal(await f.server.urlFor('exec-a', '../outside.txt'), null)
    assert.equal(await f.server.urlFor('exec-a', '.env'), null)
  } finally {
    await f.cleanup()
  }
})

test('the server is reused across runs and can be closed and restarted', async () => {
  const f = await fixture()
  try {
    const first = await f.server.urlFor('exec-a')
    const second = await f.server.urlFor('exec-a', 'index.html')
    assert.equal(new URL(first as string).port, new URL(second as string).port)
    await f.server.close()
    const restarted = await f.server.urlFor('exec-a')
    assert.equal((await raw(restarted as string, '/exec-a/')).status, 200)
  } finally {
    await f.cleanup()
  }
})
