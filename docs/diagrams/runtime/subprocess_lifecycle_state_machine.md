---
status: accepted
owner: Reticle Project
updated: 2026-09-19
---

# Worker subprocess lifecycle

```mermaid
stateDiagram-v2
    [*] --> Preparing: TaskDispatched
    Preparing --> Failed: dependency provisioning fails
    Preparing --> Running: interpreter and environment ready
    Running --> Running: stderr becomes WorkerLog
    Running --> TimedOut: task context expires
    Running --> Cancelled: execution is killed or runtime fails
    Running --> Failed: nonzero exit or invalid protocol
    Running --> Validating: process exits zero with one JSON response
    Validating --> Failed: identity, artifact, memory, mutation, or evidence invalid
    Validating --> Committing: acknowledged memory/artifact commit
    Committing --> Failed: durable result commit rejected
    Committing --> Completed: optional graph mutation accepted for delivery
    Completed --> [*]: WorkerCompleted
    Failed --> [*]: AttemptFinished / WorkerFailed
    TimedOut --> [*]: AttemptFinished / WorkerFailed
    Cancelled --> [*]: AttemptFinished
```

The Go `Worker.Execute` implementation owns process creation, bounded stdout/stderr capture, protocol validation, and acknowledged result commit. The dispatcher owns attempt identity and retry policy. Provider libraries do not perform hidden retries. A failed attempt can be retried only when it is classified as transient and the worker proves that no effect started.
