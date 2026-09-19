---
status: accepted
owner: Reticle Project
updated: 2026-09-19
---

# Studio IPC architecture

Reticle Studio is an Electron desktop application. The Go runtime and Electron main process communicate over the authenticated loopback HTTP/WebSocket control server. The sandboxed React renderer never connects to Forge or the filesystem directly; it uses the typed preload bridge in `studio/src/shared/ipc.ts`.

## Runtime to main process

Forge serializes the `RuntimeEvent` envelope over `/ws`. `studio/electron/forge/client.ts` authenticates with `.reticle/control-token`, reconnects with bounded backoff, and requests current state after connection. Forge replays `WaitlistUpdated` plus `WorkflowSnapshot` records. The snapshot is authoritative for current execution/node status; bounded event history supplies logs and historical timing.

The main-process `EventStore` in `studio/electron/forge/store.ts`:

1. normalizes execution, node, task, and agent identity;
2. strips large artifact payloads before IPC;
3. folds structural events through `studio/src/shared/projection.ts`;
4. stores bounded event and log rings;
5. batches projection and log pushes to the renderer;
6. scopes logs by selected execution and node.

Current structural lifecycle names come from `docs/specifications/event-taxonomy/v1/001-events.md`, including `WorkflowStarted`, `WorkflowSnapshot`, `NodeReady`, `TaskDispatched`, `WorkerStarted`, `WorkerCompleted`, `WorkerFailed`, `WorkflowCompleted`, and `WorkflowFailed`. Provisioning events carry operation and execution identity. Raw worker stdout is reserved for the one final protocol response; progress and model streaming arrive through `WorkerLog` events derived from stderr.

## Main process to renderer

The preload bridge exposes narrow typed groups rather than generic channel names:

- connection/process control and current projection;
- coalesced logs and log queries;
- models, artifacts, uploads, and approvals;
- guarded workspace summary/tree/file operations;
- settings and theme.

Channel constants and request/response types live in `studio/src/shared/ipc.ts`. Privileged handlers live under `studio/electron/`. The renderer runs with Node integration disabled, context isolation enabled, and sandboxing enabled.

## Consistency and limits

- The projection is a deterministic fold, but historical events are bounded and are not a durable outbox.
- Workspace reads execute in the main process, remain inside the configured checkout, reject symlinks and sensitive files, and cap recursive scans.
- Scheduled Explorer refreshes wait for the previous scan to finish. Results carry an in-renderer generation and stale responses are discarded.
- Environment Activity keys operations by `operation_id`; selected-run log scope uses the event's execution identity.
- Shared memory is not mirrored as a generic renderer-accessible blackboard. Studio learns about artifacts and lifecycle state through typed events and APIs.

Contract tests live in `studio/tests/`, while the runtime envelope and event behavior are tested under `runtime/`.
