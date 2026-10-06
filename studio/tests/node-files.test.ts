import test from 'node:test'
import assert from 'node:assert/strict'
import { applyEvents, createProjection } from '../src/shared/projection'
import type { RuntimeEvent } from '../src/shared/events'

function event(id: number, type: RuntimeEvent['type'], payload: unknown): RuntimeEvent {
  return { id, type, payload, timestamp: id, source: 'fixture', sessionId: 'one' }
}

const started = event(1, 'WorkflowStarted', { exec_id: 'run', edges: [{ from: 'build', to: 'review' }] })

function written(id: number, node: string, files: unknown): RuntimeEvent {
  return event(id, 'WorkerFilesWritten', { task_id: `run|${node}`, worker_id: 'frontend-agent', execution: 'run', attempt_id: 'a1', files })
}

test('files a node reports are attached to that node only', () => {
  const state = applyEvents(createProjection(), [started, written(2, 'build', ['index.html', 'assets/a.png'])])
  assert.deepEqual(state.runs.run.nodes.build.files, ['index.html', 'assets/a.png'])
  assert.deepEqual(state.runs.run.nodes.review.files, [])
})

test('a later report replaces an earlier attempt, and the earlier state is not mutated', () => {
  const first = applyEvents(createProjection(), [started, written(2, 'build', ['old.html'])])
  const second = applyEvents(first, [written(3, 'build', ['new.html', 'main.js'])])
  assert.deepEqual(second.runs.run.nodes.build.files, ['new.html', 'main.js'])
  assert.deepEqual(first.runs.run.nodes.build.files, ['old.html'])
})

test('malformed file lists are ignored or cleaned', () => {
  const state = applyEvents(createProjection(), [started, written(2, 'build', 'not-an-array'), written(3, 'review', ['ok.md', 7, null, 'also.md'])])
  assert.deepEqual(state.runs.run.nodes.build.files, [])
  assert.deepEqual(state.runs.run.nodes.review.files, ['ok.md', 'also.md'])
})

test('a files report that arrives before the node is known still creates it', () => {
  const state = applyEvents(createProjection(), [written(1, 'late', ['x.txt'])])
  assert.deepEqual(state.runs.run.nodes.late.files, ['x.txt'])
})

test('a text artifact keeps its text so its row can open it, other types do not', () => {
  const artifact = (id: number, type: string, data: unknown) =>
    event(id, 'ArtifactStored', { id: `art-${id}`, name: 'frontend output', type, task: 'run|build', execution: 'run', producer: 'frontend-agent', version: 1, data })
  const state = applyEvents(createProjection(), [
    started,
    artifact(2, 'document/markdown', 'Built the gallery'),
    artifact(3, 'application/json', '{"a":1}'),
  ])
  const [summary, json] = state.runs.run.nodes.build.artifacts
  assert.equal(summary.text, 'Built the gallery')
  assert.equal(json.text, undefined)
})
