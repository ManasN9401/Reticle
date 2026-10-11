import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { describeStaleBinary, staleBinary } from '../electron/forge/staleness'

function repo(): { root: string; binary: string; source: (relative: string, at: number) => string } {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'reticle-stale-'))
  const binary = path.join(root, 'cmd', 'forge', 'forge.exe')
  fs.mkdirSync(path.dirname(binary), { recursive: true })
  fs.writeFileSync(binary, 'binary')
  const source = (relative: string, at: number) => {
    const file = path.join(root, relative)
    fs.mkdirSync(path.dirname(file), { recursive: true })
    fs.writeFileSync(file, 'package x')
    fs.utimesSync(file, at / 1000, at / 1000)
    return file
  }
  return { root, binary, source }
}

const BUILT = Date.parse('2026-10-08T19:52:00Z')

test('a Go source changed after the build makes the binary stale and names the file', () => {
  const { root, binary, source } = repo()
  fs.utimesSync(binary, BUILT / 1000, BUILT / 1000)
  source('runtime/agent/old.go', BUILT - 3600_000)
  source('runtime/agent/dispatcher.go', BUILT + 2 * 86400_000)
  const stale = staleBinary(binary, root)
  assert.ok(stale)
  assert.equal(stale.source, path.join('runtime', 'agent', 'dispatcher.go'))
  assert.match(describeStaleBinary(stale), /go build -o forge\.exe/)
})

test('a binary built after the last change is current', () => {
  const { root, binary, source } = repo()
  source('runtime/agent/dispatcher.go', BUILT - 60_000)
  source('cmd/forge/main.go', BUILT - 60_000)
  fs.utimesSync(binary, BUILT / 1000, BUILT / 1000)
  assert.equal(staleBinary(binary, root), null)
})

test('files that cannot change the binary are ignored', () => {
  const { root, binary, source } = repo()
  fs.utimesSync(binary, BUILT / 1000, BUILT / 1000)
  const later = BUILT + 86400_000
  source('runtime/agent/dispatcher_test.go', later)
  source('cmd/forge/compiler/lib/worker_sdk.py', later)
  source('cmd/forge/compiler/helper.go', later)
  source('runtime/node_modules/x/y.go', later)
  assert.equal(staleBinary(binary, root), null)
})

test('a tiny gap, a missing binary and a tree without sources are not judged stale', () => {
  const { root, binary, source } = repo()
  fs.utimesSync(binary, BUILT / 1000, BUILT / 1000)
  source('runtime/a.go', BUILT + 500)
  assert.equal(staleBinary(binary, root), null)
  assert.equal(staleBinary(path.join(root, 'missing.exe'), root), null)
  const bare = fs.mkdtempSync(path.join(os.tmpdir(), 'reticle-bare-'))
  const lone = path.join(bare, 'forge.exe')
  fs.writeFileSync(lone, 'x')
  assert.equal(staleBinary(lone, bare), null)
})
