# External effect recovery

The attempt-scoped broker now uses the [durable effect manager](rfc/RFC-045-Durable-Execution-Capabilities-Effects.md) before entering MCP or plugin adapters. This implements the existing [broker effect contract](rfc/RFC-050-Attempt-Scoped-Tool-Broker.md); transport success does not establish retry safety.

Every broker call in Forge records attempt, execution, task, adapter, tool and a SHA-256 of canonical JSON arguments in `.reticle/effects/state.json`. Raw arguments, credentials and results are not stored in these records. The broker submits canonical arguments to the adapter so the fingerprint and executed request have the same representation.

An intent is persisted before the adapter starts. If persistence fails, the adapter is not entered. Completion is persisted before returning the response. Completion-persistence failure returns uncertainty and stops further broker admission until restart. Adapter panics are bounded errors with uncertain effect state. Cancellation and a revoked worker response do not erase the ledger.

Only `no_effect` outcomes can be settled generically. `effect_started` and `uncertain` remain interrupted until a trusted observer reconciles them, even if the remote call returned successfully. On restart, prepared/running records become interrupted. Equivalent unresolved calls on the same execution/task/adapter/tool/argument digest are blocked across fresh attempt IDs. Other tasks remain independent. Completed call IDs cannot be reused after restart.

The generic ledger does not infer external idempotency or automatically replay a completed operation in a new attempt. MCP tools are conservatively uncertain by default; repeated equivalent calls can therefore require reconciliation. This is deliberate until server-specific read-only/idempotency/reconciliation contracts are available. Host plugins and MCP servers remain trusted executable code.

## Operator controls

The existing authenticated, loopback-only telemetry control API exposes:

- `GET /api/effects`: latest effect records, bounded to 1,000 results, with a `truncated` flag. Optional `state` filtering occurs before the limit, so older interrupted operations remain discoverable among large completed histories.
- `POST /api/effects/reconcile`: JSON `{id, state, external_id?, observation}`. The observation is required and must describe checked external state. State must be `succeeded`, `failed`, or `cancelled`; the original record must be interrupted.

These routes require the same control authentication and same-origin checks as the other runtime controls. Reconciliation records the observation and never re-executes a tool. It is not an agent tool or an approval shortcut.

Studio exposes these controls in **Settings → External Effects**. Interrupted operations appear by default; Show all includes the completed history. Select an interrupted operation, inspect its task and operation details, then record the externally observed outcome and supporting observation. The observation is required. Failed requests remain visible and the form preserves the user's input. Disconnecting removes the active form, and stale list responses cannot replace a more recent filter selection.

For example, an operator who has queried the remote service and confirmed that an operation was never applied may reconcile it to `failed` with the lookup receipt and observation. An operator must not infer absence from a network error. A subsequent operation needs a fresh call identity and the ordinary capability/policy checks.

The store remains a single-process snapshot, not a transactional outbox or distributed ledger. Windows interrupted replacement can recover from a previous snapshot when the primary is absent. Corrupt, null or invalid-identity snapshots fail startup. Retention, external-provider reconciliation adapters and durable result replay remain outstanding.

Tests exercise the HTTP handler in process (without needing sockets), disk persistence, restart interruption, denied replay, reconciliation, failed admission, panic uncertainty and authenticated operator controls. They do not claim live MCP/provider integration or production power-loss durability.
