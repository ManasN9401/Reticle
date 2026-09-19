import test from 'node:test'
import assert from 'node:assert/strict'
import { buildLlmDiagnostics, parseLlmLog } from '../src/features/graph/llmLog'

test('parses and joins adjacent structured model diagnostics', () => {
  const diagnostics = buildLlmDiagnostics([
    { seq: 1, message: '[LLM_STREAM] {"kind":"reasoning","text":"Check "}' },
    { seq: 2, message: '[LLM_STREAM] {"kind":"reasoning","text":"inputs."}' },
    { seq: 3, message: '[LLM_STREAM] {"kind":"content","text":"{\\"agents\\":"}' },
    { seq: 4, message: '[LLM_STREAM] {"kind":"content","text":"[]}"}' },
    { seq: 5, message: '[LLM_STREAM] {"kind":"tool","text":"Requested read_file","name":"read_file"}' },
  ])

  assert.deepEqual(diagnostics.map(({ kind, text, name }) => ({ kind, text, name })), [
    { kind: 'reasoning', text: 'Check inputs.', name: undefined },
    { kind: 'content', text: '{"agents":[]}', name: undefined },
    { kind: 'tool', text: 'Requested read_file', name: 'read_file' },
  ])
})

test('keeps compatibility with historical string stream events', () => {
  assert.equal(
    parseLlmLog({ seq: 8, message: '[LLM_STREAM] "hello\\n"' })?.text,
    'hello\n',
  )
})

test('classifies legacy provider failures as model status', () => {
  const parsed = parseLlmLog({ seq: 9, message: '[LLM] Hard limit reached on model' })
  assert.equal(parsed?.kind, 'status')
})
