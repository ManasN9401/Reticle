// Offline probes of the real pure projection, transpiled using the installed TS.
// No Electron launch, package installation, network, or runtime state is used.
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.resolve(__dirname, '../../../..');
const ts = require(path.join(root, 'studio/node_modules/typescript'));
const cache = new Map();
function load(file) {
  if (cache.has(file)) return cache.get(file);
  const output = ts.transpileModule(fs.readFileSync(file, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 }
  }).outputText;
  const mod = { exports: {} };
  cache.set(file, mod.exports);
  const context = { module: mod, exports: mod.exports,
    require: name => load(path.resolve(path.dirname(file), name + '.ts')) };
  vm.runInNewContext(output, context, { filename: file });
  return mod.exports;
}
const p = load(path.join(root, 'studio/src/shared/projection.ts'));
const event = (id, type, sessionId, payload) => ({ id, type, sessionId,
  timestamp: 1000 + id, source: 'offline-audit', payload });
const start = event(1, 'WorkflowStarted', 'session-a', { exec_id: 'exec-001' });
let state = p.applyEvents(p.createProjection(), [start,
  event(2, 'ExecutionPaused', 'session-a', { execution: 'exec-001' })]);
const paused = state.runs['exec-001'].status;
state = p.applyEvents(state, [event(3, 'ExecutionKilled', 'session-a', { execution: 'exec-001' })]);
const killed = state.runs['exec-001'].status;
state = p.applyEvents(state, [event(4, 'WorkerStarted', 'session-a', { task_id: 'exec-001|old-node' }),
  event(1, 'WorkflowStarted', 'session-b', { exec_id: 'exec-001' })]);
const results = {
  status_after_pause: paused,
  status_after_kill: killed,
  old_node_survives_new_session: !!state.runs['exec-001'].nodes['old-node'],
  projection_session: state.sessionId,
  out_of_order_replay_event_count: p.replayTo([
    event(2, 'WorkflowStarted', 'session-a', { exec_id: 'exec-002' }), start
  ], 1).eventCount
};
fs.writeFileSync(path.join(__dirname, 'studio-results.json'), JSON.stringify(results, null, 2) + '\n');
console.log(JSON.stringify(results, null, 2));
