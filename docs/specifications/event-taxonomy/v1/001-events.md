---
Status: Stable
Author: HyperParallel Core
Date: 2026-07-31
---

# Event Taxonomy (v1)

This document represents the canonical list of domain events emitted and consumed by the HyperParallel framework components. Internal Go logic can change, but the emission and payload structures of these events are strictly governed.

## System Events
- **`RuntimeStarted`**
  - **Producer:** Orchestrator
  - **Payload:** None
  - **Description:** Emitted when the engine begins.

## Workflow Lifecycle
- **`NodeReady`**
  - **Producer:** WorkflowEngine
  - **Payload:** `{"exec_id": string, "node_id": string, "workflow": string}`
  - **Description:** Emitted when the graph determines all predecessors are satisfied and a node is eligible to run.

- **`TaskCreated`**
  - **Producer:** WorkflowEngine
  - **Payload:** `Task` struct (see Worker Protocol)
  - **Description:** Emitted when the physical unit of work is instantiated with dependencies resolved.

- **`TaskFailed`**
  - **Producer:** WorkflowEngine
  - **Payload:** `{"exec_id": string, "node_id": string, "task_id": string, "workflow": string}`
  - **Description:** Emitted when a node's execution has fatally failed.

- **`WorkflowCompleted`**
  - **Producer:** WorkflowEngine
  - **Payload:** `{"execution": string, "workflow": string}`
  - **Description:** Emitted when all nodes in a DAG successfully complete.

- **`WorkflowFailed`**
  - **Producer:** WorkflowEngine
  - **Payload:** `{"execution": string, "node_id": string, "reason": string, "workflow": string}`
  - **Description:** Emitted when a workflow fails (e.g., due to a fail-fast policy on a failed node).

## Dispatcher & Worker Lifecycle
- **`TaskDispatched`**
  - **Producer:** Dispatcher
  - **Payload:** `{"task_id": string, "worker_id": string}`
  - **Description:** Emitted when the Dispatcher assigns a task to a Worker.

- **`WorkerStarted`**
  - **Producer:** Worker
  - **Payload:** `{"task_id": string, "worker_id": string}`
  - **Description:** Emitted when the subprocess OS process begins.

- **`WorkerCompleted`**
  - **Producer:** Worker
  - **Payload:** `{"task_id": string, "worker_id": string}`
  - **Description:** Emitted when the subprocess exits with `0` successfully.

- **`WorkerFailed`**
  - **Producer:** Dispatcher
  - **Payload:** `{"task_id": string, "worker_id": string, "reason": string, "exit_code": int, "stderr": string}`
  - **Description:** Emitted when a worker crashes, exits non-zero, or violates the stdout JSON protocol.

## Memory & State Lifecycle
- **`ArtifactsProduced`**
  - **Producer:** Worker
  - **Payload:** `Artifact` struct
  - **Description:** Emitted by the worker wrapper when a valid artifact is parsed from stdout.

- **`ArtifactStored`** / **`ArtifactVersionCreated`**
  - **Producer:** MemoryManager
  - **Payload:** `Artifact` struct
  - **Description:** Emitted after the artifact has been safely persisted to memory.

- **`MemoryReadRequested`**
  - **Producer:** Orchestrator (or other framework components)
  - **Payload:** `{"scope": string, "scope_id": string, "key": string}`
  - **Description:** Emitted when a component asynchronously requests a memory read via the event bus.

- **`MemoryReadCompleted`**
  - **Producer:** MemoryManager
  - **Payload:** `{"scope": string, "scope_id": string, "key": string, "value": any, "found": bool}`
  - **Description:** Emitted by the memory manager in response to a read request.

- **`MemoryWriteRequested`**
  - **Producer:** Worker (or other mutating components)
  - **Payload:** `MemoryEntry` struct
  - **Description:** Emitted to mutate the Runtime State memory (e.g. for scalar or JSON object variables).

- **`MemoryUpdated`**
  - **Producer:** MemoryManager
  - **Payload:** `{"scope": string, "scope_id": string, "key": string}`
  - **Description:** Emitted when a memory entry is successfully mutated in the Runtime State memory.
