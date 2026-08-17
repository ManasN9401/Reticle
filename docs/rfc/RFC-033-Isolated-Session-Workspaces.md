# RFC-033: Isolated Session Workspaces for Concurrent Node Maps

## Status
Proposed (Future Roadmap)

## Context
In the current architecture, a "Workspace" represents both the physical project directory and the execution environment for an orchestrated workflow. When a user submits a prompt, `forge.exe` generates a `workflow.yaml` DAG. However, if a user submits *multiple* concurrent prompts (or queues them), they overwrite the single node map for the workspace, causing race conditions, state clashing, and cross-contamination of execution logic.

## Proposal
Decouple the "Project Directory" from the "Workflow Session":
1. **Global Project Scope:** The underlying codebase remains a single shared context.
2. **Isolated Session Sandboxes:** Every prompt triggers a unique `session_id`.
3. **Namespaced Workflows:** Instead of a global `workflow.yaml`, `scaffolder.py` generates `workflows/workflow_{session_id}.yaml`.
4. **Execution Isolation:** `executor.go` filters and executes sub-processes purely scoped to their `session_id`. The Event Bus will tag all events (like `AgentFinished`) with the corresponding session ID so the Telemetry UI can render multiple concurrent graph visualizations.

## Consequences
- **Pros:** Users can spawn highly complex, parallel coding instructions (e.g. "Build the UI" while simultaneously "Setup the database") without the node maps destroying each other.
- **Cons:** Shared file modification conflicts will emerge if two isolated sessions attempt to edit the same file simultaneously. Requires a File Locking or CRDT mechanism (Future RFC).
