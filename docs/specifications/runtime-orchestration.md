# Runtime Orchestration Architecture

This document specifies the core architecture of Reticle's Orchestration Engine, handled entirely by the Go backend (specifically `workflow_engine.go` and `worker.go`).

## 1. The Workflow Engine

The `WorkflowEngine` is responsible for parsing, validating, and executing Directed Acyclic Graphs (DAGs). These DAGs can be statically defined (e.g., `compile.yaml`) or generated dynamically by LLM planning agents like the Architect.

### Graph Execution
- The orchestrator builds a topological sort of the DAG.
- Nodes with no unmet dependencies are executed in parallel using goroutines.
- If a node fails, dependent nodes are cancelled or marked as blocked.

## 2. Worker Lifecycle (`worker.go`)

Each node in the DAG maps to a specific `Worker` process (usually a Python agent using `worker_sdk.py`).

1. **Context Provisioning**: The Engine resolves the assigned skills and provisions a virtual environment (via UV).
2. **Execution**: The worker is spawned as an external OS process.
3. **Data Pipes**: `stdin` is fed the initial prompt and graph context. `stdout` and `stderr` are streamed back and broadcast over the `EventBus`.
4. **Completion**: The worker must terminate on its own (typically via an LLM JSON tool call that exits the loop) or it is killed via timeout (Iteration budget exhausted).

## 3. Dynamic Execution & Hot-Swapping

When a special agent like `architect-agent` runs, it generates new `.yaml` files and graph definitions. The `WorkflowEngine` dynamically hot-reloads these into the running session state, allowing the DAG to expand mid-execution.
