> Implementation update (2026-09-08): this historical proposal is superseded where it conflicts with RFC-043 and the current specifications under docs/specifications/. See the audit repair ledger for remaining capability limits.

# RFC-041: Agent Execution Lifecycle Control (Pause/Resume/Kill)

## 1. Objective
Provide real-time lifecycle control over spawned agent workflows directly from the Studio UI, allowing users to pause, resume, or forcefully terminate runaway agents and expensive LLM loops.

## 2. Architecture

### 2.1 The Event Bus layer
The UI uses the websocket bridge to dispatch outbound commands (`action: 'kill'`, `'pause'`, `'resume'`). The telemetry server decodes these and publishes native runtime events: `ExecutionKilled`, `ExecutionPaused`, `ExecutionResumed`.

### 2.2 The Waitlist Manager
The `WaitlistManager` (`waitlist.go`) acts as the state machine. It intercepts these events and modifies the `ExecutionStatus` of the queue items.
* If `ExecutionKilled` is received: The item is marked as `StatusFailed` (aborted).
* If `ExecutionPaused` is received: The item is marked as `StatusPaused`.

### 2.3 The Dispatcher & Context Cancellation
To prevent orphaned Python subprocesses from running in the background when an execution is killed:
1. `dispatcher.go` maintains an `activeTasks` map linking `ExecutionID` -> `TaskID` -> `context.CancelFunc`.
2. When `ExecutionKilled` arrives, the dispatcher iterates through `activeTasks[executionID]` and calls all cancellation functions.
3. The underlying `exec.CommandContext` receives the interrupt, immediately terminating the python worker process.
4. The orchestrator explicitly checks `ctx.Err() != nil` upon worker return to instantly break the `maxRetries` routing loop, ensuring the task doesn't restart.
