import test from 'node:test'
import assert from 'node:assert/strict'
import { EventStore } from '../electron/forge/store'
import type { RuntimeEvent } from '../src/shared/events'

function workerLog(id: number, execution: string | undefined, log: string): RuntimeEvent {
  return {
    id,
    type: 'WorkerLog',
    timestamp: Date.now() + id,
    source: 'worker',
    sessionId: 'fixture',
    payload: { execution, worker_id: 'fixture-agent', log },
  }
}

test('export query matches the panel execution and LLM filters', () => {
  const store = new EventStore(100)
  store.ingest(workerLog(1, 'selected', 'visible selected record'))
  store.ingest(workerLog(2, 'other', 'hidden other record'))
  store.ingest(workerLog(3, undefined, 'visible global record'))
  store.ingest(workerLog(4, 'selected', '[LLM_STREAM] {"kind":"content","text":"hidden"}'))

  const result = store.queryLogs({
    execId: 'selected',
    includeUnscoped: true,
    excludeLlm: true,
  })

  assert.deepEqual(result.records.map((record) => record.message), [
    'visible selected record',
    'visible global record',
  ])
  store.dispose()
})
