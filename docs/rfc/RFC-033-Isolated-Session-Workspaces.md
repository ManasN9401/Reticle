---
status: historical
owner: Reticle Project
updated: 2026-09-19
---

> Historical design record. Current versioned specifications and schemas take precedence.

> Implementation update (2026-09-08): this historical proposal is superseded where it conflicts with RFC-043 and the current specifications under docs/specifications/. See the audit repair ledger for remaining capability limits.

# RFC-033: Isolated Session Workspaces for Concurrent Node Maps

## Status
Accepted

## Context
In the current architecture, a "Workspace" represents both the physical project directory and the execution environment for an orchestrated workflow. When a user submits a prompt, `forge.exe` generates a `workflow.yaml` DAG. However, if a user submits *multiple* concurrent prompts (or queues them via the Waitlist), they overwrite the single node map for the workspace, causing race conditions, state clashing, and cross-contamination of execution logic.

## Proposal
We will decouple the "Project Directory" from the "Workflow Session Sandboxes".

### 1. Directory Structure Overhaul
We will transition from a singular global `.reticle` workspace to a nested, session-based execution environment.
- **Global Project Scope:** The underlying codebase (`src/`, `cmd/`, etc.) remains a single shared context.
- **Isolated Session Sandboxes:** Every execution triggered by the Waitlist generates a unique `session_id` (e.g., `exec-001`).
- **Namespaced Metadata:** `scaffolder.py` will no longer generate a global `workflow.yaml`. Instead, it will write to `.reticle/sessions/{session_id}/workflow.yaml`. All artifacts and transient memory states will be strictly bound to this directory.

### 2. Event Bus & Memory Segregation
- The `WaitlistItem` ID (e.g., `exec-001`) becomes the universal `session_id`.
- Every event published to the Go Event Bus (`NodeStarted`, `AgentFinished`, `MemoryWriteRequested`) MUST include this `session_id` in its payload.
- The `MemoryStore`'s `ScopeExecution` namespace will enforce strict boundaries. An agent running in `exec-001` cannot read the dynamic memory keys or intermediate artifacts of `exec-002`.

### 3. Concurrency Safety: Pessimistic File Locking
If two isolated sessions (e.g., Prompt A: "Refactor auth" and Prompt B: "Add rate limiting") run concurrently, they may attempt to modify the same source file (`middleware.go`) simultaneously. If Agent B generates a line-replacement patch based on a stale read of `middleware.go` (before Agent A applied its changes), the patch will corrupt the file.

To prevent "messy merges" or stale patches, we introduce **Pessimistic File Locking**:
1. **Lock Request:** When an agent decides it needs to edit a file, it must emit a `FileLockRequested` JSON payload to the Go Orchestrator via `stdout`.
2. **Orchestrator Mutex:** The Go Event Bus maintains an in-memory `sync.Mutex` map keyed by absolute file paths.
3. **Blocking Execution:** If Agent B requests a lock on `middleware.go` while Agent A holds it, the Orchestrator pauses Agent B's execution stream.
4. **Fresh Context:** Once Agent A releases the lock, Agent B acquires it. Crucially, Agent B must now read the *fresh* state of the file from disk before attempting to write any code, ensuring its context is completely up-to-date.

### 4. Agent Workspace Pre-flight Context Injection
Because sessions are completely isolated, agents must have absolute certainty about the current state of their internal `src/` directory without wasting LLM turns running blind exploratory commands.

Before invoking the LLM, the Python worker script (`coder.py`) will automatically scan the isolated `src/` folder for the session. It recursively builds a visual text-based file tree representing the current codebase topology. This topology is injected directly into the bottom of the System Prompt under `## Workspace State`.

This permanently eliminates the need for agents to start their workflow by executing `list_dir` commands, heavily reducing token usage and speeding up task execution.

## Consequences
- **Pros:** Users can spawn highly complex, parallel coding instructions continuously. The orchestrator guarantees that node maps never clash and file modifications are safely serialized without resorting to thousands of temporary Git branches.
- **Cons:** High-contention files (e.g., `main.go`) may cause localized bottlenecks where multiple agents sit idle waiting for locks.
