import test from 'node:test'
import assert from 'node:assert/strict'
import { applyEvents, createProjection, replayTo } from '../src/shared/projection'
import type { RuntimeEvent } from '../src/shared/events'

function event(id: number, type: RuntimeEvent['type'], payload: unknown, sessionId='one'): RuntimeEvent {
  return {id,type,payload,timestamp:id,source:'fixture',sessionId}
}

test('reconnect restores terminal node state from snapshot', () => {
  const state=applyEvents(createProjection(),[event(1,'WorkflowSnapshot',{exec_id:'run',workflow_id:'fixture',status:'completed',nodes:{solo:'done'},edges:[]})])
  assert.equal(state.runs.run.status,'completed')
  assert.equal(state.runs.run.nodes.solo.status,'done')
})

test('a batch spanning sessions does not retain the older run', () => {
  const state=applyEvents(createProjection(),[
    event(200,'WorkflowStarted',{exec_id:'old'}),
    event(1,'WorkflowStarted',{exec_id:'new'},'two'),
  ])
  assert.equal(state.runs.old,undefined)
  assert.equal(state.runs.new.status,'running')
  assert.equal(state.lastEventId,1)
})

test('pause and cancellation are terminal-aware and immutable', () => {
  const before=applyEvents(createProjection(),[event(1,'WorkflowStarted',{exec_id:'run'})])
  const paused=applyEvents(before,[event(2,'ExecutionPaused',{execution:'run'})])
  assert.equal(before.runs.run.status,'running')
  assert.equal(paused.runs.run.status,'paused')
  const killed=applyEvents(paused,[event(3,'ExecutionKilled',{execution:'run'}),event(4,'ExecutionResumed',{execution:'run'})])
  assert.equal(killed.runs.run.status,'cancelled')
})

test('replay does not stop at an out-of-order event', () => {
  const state=replayTo([event(8,'WorkflowStarted',{exec_id:'later'}),event(2,'WorkflowStarted',{exec_id:'early'})],3)
  assert.ok(state.runs.early)
  assert.equal(state.runs.later,undefined)
})

test('runtime overload fails active runs explicitly',()=>{
  const state=applyEvents(createProjection(),[
    event(1,'WorkflowStarted',{exec_id:'run'}),event(2,'RuntimeOverloaded',{restart_required:true}),
  ])
  assert.equal(state.runs.run.status,'failed')
  assert.match(state.runs.run.failureReason??'',/restart required/)
})

test('persistence failure interrupts active work explicitly',()=>{
  const state=applyEvents(createProjection(),[
    event(1,'WorkflowStarted',{exec_id:'run'}),
    event(2,'NodeReady',{exec_id:'run',node_id:'active'}),
    event(3,'WorkerStarted',{task_id:'run|active',worker_id:'fixture'}),
    event(4,'RuntimePersistenceFailed',{restart_required:true}),
  ])
  assert.equal(state.runs.run.status,'interrupted')
  assert.equal(state.runs.run.nodes.active.status,'interrupted')
})
