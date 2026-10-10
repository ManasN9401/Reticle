import test from 'node:test'
import assert from 'node:assert/strict'
import { mergeLogRecords } from '../src/shared/logMerge'
import type { LogRecord } from '../src/shared/ipc'

function record(seq: number, message = `m${seq}`): LogRecord {
  return { seq, at: seq, level: 'info', message, isLlm: false }
}

test('overlapping history and live records are merged once, oldest first', () => {
  const live = [record(5), record(6), record(7)]
  const history = [record(3), record(4), record(5, 'newer copy'), record(6)]
  const merged = mergeLogRecords(live, history, 100)
  assert.deepEqual(merged.map((r) => r.seq), [3, 4, 5, 6, 7])
  assert.equal(merged.find((r) => r.seq === 5)?.message, 'newer copy')
})

test('the newest records are kept when the limit is exceeded', () => {
  const merged = mergeLogRecords([record(1), record(2)], [record(3), record(4)], 3)
  assert.deepEqual(merged.map((r) => r.seq), [2, 3, 4])
})

test('nothing incoming returns the existing array untouched', () => {
  const existing = [record(1)]
  assert.equal(mergeLogRecords(existing, [], 10), existing)
})
