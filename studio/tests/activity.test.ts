import test from 'node:test'
import assert from 'node:assert/strict'
import { buildDependencyOperations } from '../src/features/activity/dependencyActivity'
import type { LogRecord } from '../src/shared/ipc'

function record(seq: number, message: string): LogRecord {
  return { seq, at: seq * 1000, level: 'info', message, isLlm: false }
}

test('dependency activity coalesces install and ready reports', () => {
  const operations=buildDependencyOperations([
    record(1,'Installing agent dependencies (agent_id: run__coder, deps: [requests==2.34.2 litellm==1.99.0], using_uv: true)'),
    record(2,'Agent dependencies ready (duration_ms: 750, cached: false, agent_id: run__coder, using_uv: true, deps: [requests==2.34.2 litellm==1.99.0])'),
  ])
  assert.equal(operations.length,1)
  assert.equal(operations[0].status,'ready')
  assert.equal(operations[0].manager,'uv')
  assert.equal(operations[0].durationMs,750)
  assert.deepEqual(operations[0].dependencies,['requests==2.34.2','litellm==1.99.0'])
})

test('dependency activity distinguishes cached base environments', () => {
  const operations=buildDependencyOperations([
    record(1,'Base dependencies ready (deps: [litellm==1.99.0], cached: true, using_uv: false)'),
  ])
  assert.equal(operations[0].key,'base')
  assert.equal(operations[0].cached,true)
  assert.equal(operations[0].manager,'pip')
})

test('dependency activity keeps concurrent operations for the same agent separate', () => {
  const operations=buildDependencyOperations([
    record(1,'Installing agent dependencies (operation_id: run-a/attempt:environment:agent:coder, agent_id: coder, deps: [requests==2.34.2], using_uv: true)'),
    record(2,'Installing agent dependencies (operation_id: run-b/attempt:environment:agent:coder, agent_id: coder, deps: [requests==2.34.2], using_uv: true)'),
    record(3,'Agent dependencies ready (operation_id: run-a/attempt:environment:agent:coder, agent_id: coder, deps: [requests==2.34.2], using_uv: true, cached: false)'),
  ])
  assert.equal(operations.length,2)
  assert.equal(operations.find((operation)=>operation.operationId?.startsWith('run-a'))?.status,'ready')
  assert.equal(operations.find((operation)=>operation.operationId?.startsWith('run-b'))?.status,'installing')
})
