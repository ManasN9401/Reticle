import test from 'node:test'
import assert from 'node:assert/strict'
import { buildToolCalls } from '../src/features/activity/toolActivity'
import type { ToolRecord } from '../src/features/activity/toolActivity'

let seq = 0
function tool(node: string, text: string, name?: string): ToolRecord {
  seq += 1
  const payload = JSON.stringify({ kind: 'tool', text, ...(name ? { name } : {}) })
  return { seq, at: seq, execId: 'run', nodeId: node, agentId: node, message: `[LLM_STREAM] ${payload}`, isLlm: true }
}

test('requested then completed becomes one finished call', () => {
  const calls = buildToolCalls([tool('a', 'Requested write_file'), tool('a', '\nCompleted write_file')])
  assert.equal(calls.length, 1)
  assert.deepEqual([calls[0].name, calls[0].status, calls[0].nodeId], ['write_file', 'done', 'a'])
})

test('a failure keeps its detail and a call with no result yet is running', () => {
  const calls = buildToolCalls([
    tool('a', 'Requested read_file'),
    tool('a', '\nFailed read_file: Error reading file: not found'),
    tool('a', 'Requested list_dir'),
  ])
  assert.deepEqual(calls.map((c) => c.status), ['failed', 'running'])
  assert.equal(calls[0].detail, 'Error reading file: not found')
})

test('parallel nodes calling the same tool are paired within their own node', () => {
  const calls = buildToolCalls([
    tool('a', 'Requested write_file'),
    tool('b', 'Requested write_file'),
    tool('b', '\nCompleted write_file'),
  ])
  assert.deepEqual(calls.map((c) => [c.nodeId, c.status]), [['a', 'running'], ['b', 'done']])
})

test('an outcome whose request was evicted is still shown', () => {
  const calls = buildToolCalls([tool('a', '\nCompleted generate_local_asset')])
  assert.deepEqual(calls.map((c) => [c.name, c.status]), [['generate_local_asset', 'done']])
})

test('legacy [TOOL] lines and non-tool model output are handled', () => {
  const legacy: ToolRecord = { seq: 900, at: 900, message: '[TOOL] Checkpoint file created at: x', isLlm: false }
  const reasoning: ToolRecord = { seq: 901, at: 901, message: '[LLM_STREAM] {"kind":"reasoning","text":"thinking"}', isLlm: true }
  const calls = buildToolCalls([legacy, reasoning])
  assert.equal(calls.length, 1)
  assert.equal(calls[0].status, 'done')
  assert.match(calls[0].name, /^Checkpoint file created/)
})
