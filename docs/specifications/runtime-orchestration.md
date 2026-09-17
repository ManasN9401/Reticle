# Runtime Orchestration Architecture

This document specifies the core architecture of Reticle's Orchestration Engine, handled entirely by the Go backend (specifically `workflow_engine.go` and `worker.go`).

## 1. The Workflow Engine

The `WorkflowEngine` is responsible for parsing, validating, and executing Directed Acyclic Graphs (DAGs). These DAGs can be statically defined (e.g., `compile.yaml`) or generated dynamically by LLM planning agents like the Architect.

### Graph Execution
- The orchestrator builds a topological sort of the DAG.
- Nodes with no unmet dependencies are executed in parallel using goroutines.
- If a node fails, dependent nodes are cancelled or marked as blocked.
- Executions, graph revisions, node states and attempts are persisted under `.reticle/executions/`.
- Work that was running when the process stopped recovers as `interrupted` and requires an explicit reconciliation decision.
- Dynamic delegation requires the active attempt ID, a registered target worker and remaining node budget before the revised graph is committed.

## 2. Worker Lifecycle (`worker.go`)

Each node in the DAG maps to a specific `Worker` process (usually a Python agent using `worker_sdk.py`).

1. **Context Provisioning**: The Engine resolves the assigned skills and provisions a virtual environment (via UV).
2. **Execution**: The worker is spawned as an external OS process.
3. **Data Pipes**: `stdin` is fed the initial prompt and graph context. `stdout` and `stderr` are streamed back and broadcast over the `EventBus`.
4. **Completion**: The worker must terminate on its own (typically via an LLM JSON tool call that exits the loop) or it is killed via timeout (Iteration budget exhausted).

Every concrete try has a random attempt ID. The dispatcher owns retry policy and completion publication. Accepted result mutations use the attempt ID as a durable idempotency key so duplicate delivery cannot apply them twice.

Workers receive resolved capabilities from their manifest. The shared SDK filters its tool definitions from those grants. Native execution also requires the user's native setting.

## 3. Dynamic Execution & Hot-Swapping

When a supervisor requests delegation, the `WorkflowEngine` validates and commits new execution-scoped nodes and edges. Graph changes do not hot-reload global agent definitions. See [RFC-045](../rfc/RFC-045-Durable-Execution-Capabilities-Effects.md) for restart and external-effect semantics.
