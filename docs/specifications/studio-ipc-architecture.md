# Studio IPC Architecture

This document specifies the inter-process communication (IPC) boundary between the Reticle Go Backend (Graph Engine & Orchestrator) and the React Frontend (Studio UI). 

## 1. Overview

The Reticle Studio operates as an Electron-like desktop application (or web client) that heavily relies on a unidirectional and bi-directional event stream to maintain synchronization with the local Go runtime. The core bridge is established over WebSockets and native IPC channels.

## 2. Event Ingestion Pipeline

The Go Backend emits structured events using an `EventBus`. The frontend subscribes to these events via the `ipc.ts` bridge, which maps backend structs to frontend TypeScript interfaces.

### Backend Emission (`dispatcher.go` / `worker.go`)
When a node starts, logs a chunk, or completes, it publishes to the bus:
```go
w.Bus.Publish("WorkerLog", "worker", map[string]any{"task_id": req.ID, "worker_id": w.ID, "log": line})
```

### Frontend Bridge (`ipc.ts`)
The `ipc.ts` module sets up listeners (e.g. `bridge.logs.onBatch`) that receive these payloads. The frontend batches these events to minimize React re-renders.

## 3. Core IPC Channels

1. **`workflow_events`**: Propagates major DAG lifecycle events (e.g., `WorkflowStarted`, `NodeStarted`, `NodeCompleted`, `NodeFailed`).
2. **`worker_logs`**: Streams `stdout` and `stderr` directly from the Python sub-processes into the Studio `store.ts`.
3. **`memory_sync`**: Synchronizes the shared memory blackboard (KV store) so the UI can accurately reflect artifact production and state changes.

## 4. State Projection (`store.ts`)

Instead of tightly coupling UI components to IPC events, Reticle uses a projection model:
1. `ipc.ts` receives raw JSON events.
2. Events are pushed to a ring-buffer/Zustand store in `store.ts`.
3. Components like `Inspector.tsx` and `RunsSidebar.tsx` reactively select only the slice of state they care about (e.g., filtering logs by `agentId`).

This architecture ensures the React UI remains highly responsive even when the Go backend is emitting thousands of LLM token streams per second.
