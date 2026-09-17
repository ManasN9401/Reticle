import test from 'node:test'
import assert from 'node:assert/strict'
import { EventStore } from '../electron/forge/store'
import type { RuntimeEvent } from '../src/shared/events'
import type { LogBatch } from '../src/shared/ipc'

test('shared environment lifecycle remains visible under run log scope', async () => {
  const store=new EventStore(100)
  store.setScope({execId:'selected-run'})
  const batch=new Promise<LogBatch>((resolve) => store.once('logs',resolve))
  const event: RuntimeEvent={
    id:1,
    type:'EnvironmentProvisioningStarted',
    timestamp:Date.now(),
    source:'environment',
    sessionId:'fixture',
    payload:{scope:'agent',agent_id:'other-run__coder',dependencies:['requests==2.34.2'],using_uv:true,cached:false},
  }
  store.ingest(event)
  const result=await batch
  store.dispose()
  assert.equal(result.records.length,1)
  assert.match(result.records[0].message,/Installing agent dependencies/)
  assert.match(result.records[0].message,/requests==2\.34\.2/)
})
