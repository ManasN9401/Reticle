import test from 'node:test'
import assert from 'node:assert/strict'
import { createProjection } from '../src/shared/projection'
import type { ProjectionState, RunStatus } from '../src/shared/projection'
import { chooseProduct, detectCompletions, formatRunDuration } from '../src/shared/runCompletion'

function projection(runs: Record<string, RunStatus>): ProjectionState {
  const state = createProjection()
  for (const [execId, status] of Object.entries(runs)) {
    state.runs[execId] = { execId, status, nodes: {}, edges: [] }
    state.runOrder.push(execId)
  }
  return state
}

test('a running run that completes is reported once', () => {
  const before = projection({ 'exec-a': 'running' })
  const after = projection({ 'exec-a': 'completed' })
  assert.deepEqual(
    detectCompletions(before, after).map((item) => [item.execId, item.outcome]),
    [['exec-a', 'completed']],
  )
  assert.deepEqual(detectCompletions(after, after), [])
})

test('failed runs are reported as failures', () => {
  const found = detectCompletions(projection({ 'exec-a': 'running' }), projection({ 'exec-a': 'failed' }))
  assert.equal(found[0].outcome, 'failed')
})

test('there is no toast for the first projection, replays or runs first seen already finished', () => {
  const finished = projection({ 'exec-old': 'completed' })
  assert.deepEqual(detectCompletions(null, finished), [])
  assert.deepEqual(detectCompletions(undefined, finished), [])
  assert.deepEqual(detectCompletions(projection({}), finished), [])
})

test('compile-phase, cancelled and interrupted runs are ignored', () => {
  const before = projection({ 'compile-exec-a': 'running', 'exec-b': 'running', 'exec-c': 'running' })
  const after = projection({ 'compile-exec-a': 'completed', 'exec-b': 'cancelled', 'exec-c': 'interrupted' })
  assert.deepEqual(detectCompletions(before, after), [])
})

test('only the runs that changed are reported when several are present', () => {
  const before = projection({ 'exec-a': 'completed', 'exec-b': 'running' })
  const after = projection({ 'exec-a': 'completed', 'exec-b': 'completed' })
  assert.deepEqual(detectCompletions(before, after).map((item) => item.execId), ['exec-b'])
})

test('the preview entry prefers the root index.html, then any index, then the shallowest page', () => {
  assert.equal(chooseProduct(['a/index.html', 'index.html', 'about.html']).previewEntry, 'index.html')
  assert.equal(chooseProduct(['pages/deep/index.html', 'public/index.html']).previewEntry, 'public/index.html')
  assert.equal(chooseProduct(['z.html', 'sub/a.html']).previewEntry, 'z.html')
  assert.equal(chooseProduct(['main.py', 'README.md']).previewEntry, undefined)
})

test('product files exclude lockfiles and generated or index folders, and lead with the entry page', () => {
  const product = chooseProduct([
    'package-lock.json',
    'node_modules/x/index.js',
    '.rag/state.json',
    'src/app.js',
    'style.css',
    'index.html',
    'images/a.png',
    'public\\site.css',
  ])
  assert.deepEqual(product.codeFiles, ['index.html', 'style.css', 'public/site.css', 'src/app.js'])
})

test('durations are readable', () => {
  assert.equal(formatRunDuration(42_400), '42s')
  assert.equal(formatRunDuration(303_000), '5m 3s')
  assert.equal(formatRunDuration(3_720_000), '1h 2m')
  assert.equal(formatRunDuration(-5), '0s')
})
