import test from 'node:test'
import assert from 'node:assert/strict'
import { EventStore } from '../electron/forge/store'
import type { RuntimeEvent } from '../src/shared/events'
import type { LogBatch } from '../src/shared/ipc'

test('environment lifecycle is scoped to its execution', async () => {
  const store=new EventStore(100)
  store.setScope({execId:'selected-run'})
  const batch=new Promise<LogBatch>((resolve) => store.once('logs',resolve))
  const other: RuntimeEvent={
    id:1,
    type:'EnvironmentProvisioningStarted',
    timestamp:Date.now(),
    source:'environment',
    sessionId:'fixture',
    payload:{scope:'agent',execution:'other-run',operation_id:'other/attempt:environment:agent:coder',agent_id:'coder',dependencies:['requests==2.34.2'],using_uv:true,cached:false},
  }
  const selected: RuntimeEvent={
    ...other,
    id:2,
    payload:{scope:'agent',execution:'selected-run',operation_id:'selected/attempt:environment:agent:coder',agent_id:'coder',dependencies:['requests==2.34.2'],using_uv:true,cached:false},
  }
  store.ingest(other)
  store.ingest(selected)
  const result=await batch
  store.dispose()
  assert.equal(result.records.length,1)
  assert.equal(result.records[0].execId,'selected-run')
  assert.match(result.records[0].message,/Installing agent dependencies/)
  assert.match(result.records[0].message,/selected\/attempt:environment:agent:coder/)
  assert.match(result.records[0].message,/requests==2\.34\.2/)
})
