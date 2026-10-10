import test from 'node:test'
import assert from 'node:assert/strict'
import { MAX_PLAN_STEPS, buildPlan, formatWaiting, parseCheckpoint } from '../src/features/hitl/planModel'
import type { Run, RunNode } from '../src/shared/projection'

function node(id: string, status: RunNode['status'] = 'pending'): RunNode {
  return { execId: 'run', nodeId: id, taskId: `run|${id}`, label: id, status, attempts: 0, artifacts: [], files: [], mocked: false, logCount: 0 }
}

function run(ids: string[], edges: [string, string][]): Run {
  return {
    execId: 'run', status: 'running', nodes: Object.fromEntries(ids.map((id) => [id, node(id)])),
    edges: edges.map(([from, to]) => ({ from, to })),
  }
}

// The shape runtime/agent/approval.go writes: a readable preface, then one JSON line in a fence.
const REQUEST = [
  '# Approval required', '', '## Goal', 'Build a gallery', '', '> quoted summary', '',
  'Review the exact task inputs below. The decision is stored separately.', '',
  '```json',
  JSON.stringify({
    task: 'run|review', prompt: 'Build a gallery', protected_action: { kind: 'deploy' },
    inputs: [{ artifact_id: 'run|design_output', version: 1, name: 'coding output', data: '### Files\n- spec.md' }],
  }),
  '```', '',
].join('\n')

test('the structured request is read from its json block, ignoring the preface', () => {
  const checkpoint = parseCheckpoint(REQUEST)
  assert.equal(checkpoint.parsed, true)
  assert.equal(checkpoint.prompt, 'Build a gallery')
  assert.deepEqual(checkpoint.protectedAction, { kind: 'deploy' })
  assert.deepEqual(checkpoint.inputs, [{ name: 'coding output', artifactId: 'run|design_output', data: '### Files\n- spec.md' }])
})

test('windows line endings and an unterminated fence still parse', () => {
  assert.equal(parseCheckpoint(REQUEST.replace(/\n/g, '\r\n')).parsed, true)
  assert.equal(parseCheckpoint(REQUEST.replace(/\n```\n$/, '\n')).parsed, true)
})

test('a missing, malformed or non-object request never throws', () => {
  for (const text of ['', 'no fence here', '```json\n{not json\n```', '```json\n[1,2]\n```', '```json\n"text"\n```']) {
    const checkpoint = parseCheckpoint(text)
    assert.deepEqual([checkpoint.parsed, checkpoint.inputs.length, checkpoint.prompt], [false, 0, undefined], text)
  }
  assert.deepEqual(parseCheckpoint('```json\n{"inputs":[1,null,{"data":"x"}]}\n```').inputs, [{ name: undefined, artifactId: undefined, data: 'x' }])
})

test('the steps after a checkpoint are grouped into parallel waves in execution order', () => {
  // design -> review -> core -> (env, nav) -> art -> final, with nav also feeding final
  const plan = buildPlan(
    run(['design', 'review', 'core', 'env', 'nav', 'art', 'final'], [
      ['design', 'review'], ['review', 'core'], ['core', 'env'], ['core', 'nav'],
      ['env', 'art'], ['art', 'final'], ['nav', 'final'],
    ]),
    'review',
  )
  assert.deepEqual(plan.upstream.map((n) => n.nodeId), ['design'])
  assert.deepEqual(plan.waves.map((wave) => wave.map((n) => n.nodeId)), [['core'], ['env', 'nav'], ['art'], ['final']])
  assert.equal(plan.hidden, 0)
})

test('a step that joins two branches waits for the longer one', () => {
  const plan = buildPlan(run(['gate', 'a', 'b', 'c', 'join'], [['gate', 'a'], ['gate', 'join'], ['a', 'b'], ['b', 'join'], ['gate', 'c']]), 'gate')
  assert.deepEqual(plan.waves.map((wave) => wave.map((n) => n.nodeId)), [['a', 'c'], ['b'], ['join']])
})

test('nodes before or beside the checkpoint are not listed as next steps', () => {
  const plan = buildPlan(run(['early', 'gate', 'late', 'other'], [['early', 'gate'], ['gate', 'late']]), 'gate')
  assert.deepEqual(plan.waves.flat().map((n) => n.nodeId), ['late'])
  assert.deepEqual(plan.upstream.map((n) => n.nodeId), ['early'])
})

test('a checkpoint with nothing after it has no waves, and edges to unknown nodes are ignored', () => {
  const plan = buildPlan(run(['a', 'gate'], [['a', 'gate'], ['gate', 'ghost'], ['ghost', 'a']]), 'gate')
  assert.deepEqual(plan.waves, [])
  assert.deepEqual(plan.upstream.map((n) => n.nodeId), ['a'])
})

test('a cycle cannot hang the model', () => {
  const plan = buildPlan(run(['gate', 'x', 'y'], [['gate', 'x'], ['x', 'y'], ['y', 'x']]), 'gate')
  assert.ok(plan.waves.flat().length <= 2)
})

test('a very long workflow is capped with a count of the rest', () => {
  const ids = ['gate', ...Array.from({ length: MAX_PLAN_STEPS + 15 }, (_, i) => `n${String(i).padStart(3, '0')}`)]
  const plan = buildPlan(run(ids, ids.slice(1).map((id) => ['gate', id] as [string, string])), 'gate')
  assert.equal(plan.waves.flat().length, MAX_PLAN_STEPS)
  assert.equal(plan.hidden, 15)
})

test('the outline lists every node in workflow order and marks the checkpoint', () => {
  const plan = buildPlan(run(['b', 'a', 'gate'], [['a', 'gate'], ['gate', 'b']]), 'gate')
  assert.deepEqual(plan.outline.map((entry) => [entry.node.nodeId, entry.depth, entry.here]), [['a', 0, false], ['gate', 1, true], ['b', 2, false]])
})

test('waiting time reads naturally', () => {
  assert.deepEqual([40_000, 300_000, 11_520_000, 90_000_000, -5].map(formatWaiting), ['40s', '5m', '3h 12m', '1d 1h', '0s'])
})
