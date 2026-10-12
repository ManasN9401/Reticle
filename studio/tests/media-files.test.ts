import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import http from 'node:http'
import os from 'node:os'
import path from 'node:path'
import { createPreviewServer, resolveRunFile } from '../electron/preview/server'
import { describeSniffed, sniffBinary } from '../src/shared/binarySniff'

const PNG = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 13, 0x49, 0x48, 0x44, 0x52])
const JPEG = Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0, 16, 0x4a, 0x46, 0x49, 0x46])
const GLB = Buffer.concat([Buffer.from('glTF'), Buffer.from([2, 0, 0, 0, 20, 0, 0, 0])])
const WEBP = Buffer.concat([Buffer.from('RIFF'), Buffer.from([0, 0, 0, 0]), Buffer.from('WEBPVP8 ')])

test('bytes are identified by their content, not their name', () => {
  assert.deepEqual(sniffBinary(PNG), { kind: 'png', mime: 'image/png', image: true })
  assert.equal(sniffBinary(JPEG).mime, 'image/jpeg')
  assert.equal(sniffBinary(Buffer.from('GIF89a....')).mime, 'image/gif')
  assert.equal(sniffBinary(WEBP).mime, 'image/webp')
  assert.deepEqual(sniffBinary(GLB), { kind: 'glb', mime: 'model/gltf-binary', image: false })
  assert.equal(sniffBinary(Buffer.from('  \n{"asset":{"version":"2.0"},"scenes":[]}')).kind, 'gltf')
  // JSON that is not glTF, text, and nothing at all are unknown rather than guessed.
  for (const bytes of [Buffer.from('{"name":"x"}'), Buffer.from('hello'), Buffer.alloc(0), Buffer.from([0x89, 0x50])]) {
    assert.equal(sniffBinary(bytes).kind, 'unknown')
  }
  assert.equal(describeSniffed(sniffBinary(PNG)), 'PNG')
})

function request(base: string, requestPath: string): Promise<{ status: number; type?: string }> {
  return new Promise((resolve, reject) => {
    const url = new URL(base)
    http
      .get({ host: url.hostname, port: url.port, path: requestPath }, (response) => {
        response.resume()
        response.on('end', () => resolve({ status: response.statusCode ?? 0, type: response.headers['content-type'] }))
      })
      .on('error', reject)
  })
}

async function runFolder(): Promise<{ sessions: string; src: string; rootFor: (id: string) => string | null }> {
  const sessions = fs.mkdtempSync(path.join(os.tmpdir(), 'reticle-media-'))
  const src = path.join(sessions, 'exec-m', 'src')
  fs.mkdirSync(path.join(src, 'assets', 'textures'), { recursive: true })
  fs.mkdirSync(path.join(src, 'assets', 'models'), { recursive: true })
  // The observed run: "jpg" and "glb" files that are PNG data, plus honest ones.
  fs.writeFileSync(path.join(src, 'assets', 'textures', 'floor.jpg'), PNG)
  fs.writeFileSync(path.join(src, 'assets', 'textures', 'real.png'), JPEG)
  fs.writeFileSync(path.join(src, 'assets', 'models', 'frame.glb'), PNG)
  fs.writeFileSync(path.join(src, 'assets', 'models', 'real.glb'), GLB)
  fs.writeFileSync(path.join(src, 'assets', 'models', 'real.gltf'), '{"asset":{"version":"2.0"}}')
  fs.writeFileSync(path.join(src, 'assets', 'textures', 'unknown.jpg'), 'not an image at all')
  fs.writeFileSync(path.join(src, 'logo.svg'), '<svg xmlns="http://www.w3.org/2000/svg"/>')
  fs.writeFileSync(path.join(src, '.env'), 'SECRET=1')
  fs.writeFileSync(path.join(sessions, 'exec-m', 'outside.txt'), 'outside')
  return { sessions, src, rootFor: (id) => (id === 'exec-m' ? src : null) }
}

test('a file is served as what its bytes are, and declared types hold when the bytes say nothing', async () => {
  const folder = await runFolder()
  const server = createPreviewServer(folder.rootFor)
  try {
    const base = ((await server.urlFor('exec-m')) as string).replace(/\/exec-m\/$/, '')
    const typeOf = async (p: string) => (await request(base, `/exec-m/${p}`)).type
    assert.equal(await typeOf('assets/textures/floor.jpg'), 'image/png') // PNG data under a .jpg name
    assert.equal(await typeOf('assets/textures/real.png'), 'image/jpeg') // JPEG data under a .png name
    assert.equal(await typeOf('assets/models/frame.glb'), 'image/png') // an "image saved as a model"
    assert.equal(await typeOf('assets/models/real.glb'), 'model/gltf-binary')
    assert.equal(await typeOf('assets/models/real.gltf'), 'model/gltf+json')
    assert.equal(await typeOf('assets/textures/unknown.jpg'), 'image/jpeg')
    assert.equal(await typeOf('logo.svg'), 'image/svg+xml')
  } finally {
    await server.close()
    fs.rmSync(folder.sessions, { recursive: true, force: true })
  }
})

test('run files are read through the same guards as the preview server', async () => {
  const folder = await runFolder()
  try {
    const ok = await resolveRunFile(folder.rootFor, 'exec-m', 'assets/models/real.glb')
    assert.ok(ok)
    assert.equal(ok.size, GLB.length)
    assert.deepEqual(fs.readFileSync(ok.target), GLB)
    for (const bad of ['.env', '../outside.txt', 'assets/../../outside.txt', 'assets', '', 'assets/missing.glb', 'assets\models\real.glb', 'assets/%2e%2e/x']) {
      assert.equal(await resolveRunFile(folder.rootFor, 'exec-m', bad), null, JSON.stringify(bad))
    }
    assert.equal(await resolveRunFile(folder.rootFor, 'exec-other', 'logo.svg'), null)
    assert.equal(await resolveRunFile(folder.rootFor, '../exec-m', 'logo.svg'), null)
  } finally {
    fs.rmSync(folder.sessions, { recursive: true, force: true })
  }
})
