---
status: accepted
owner: Reticle Project
updated: 2026-09-19
---

# End-to-end orchestrator sequence

```mermaid
sequenceDiagram
    participant User
    participant Waitlist
    participant Memory
    participant Graph as GraphEngine
    participant Dispatcher
    participant Worker
    participant Studio

    User->>Waitlist: enqueue prompt and settings
    Waitlist->>Memory: WriteBatch(run and compile context)
    Memory-->>Waitlist: durable acknowledgement
    Waitlist->>Graph: SubmitWorkflow(compiler or selected workflow)
    Graph-->>Studio: WorkflowStarted / NodeReady
    Graph->>Dispatcher: TaskCreated
    Dispatcher-->>Studio: AttemptStarted / TaskDispatched
    Dispatcher->>Worker: one JSON task on stdin
    Worker-->>Studio: WorkerStarted / WorkerLog
    Worker->>Memory: TaskResultCommitRequested
    Memory-->>Worker: commit accepted
    Worker-->>Studio: WorkerVerificationRecorded
    Dispatcher-->>Studio: AttemptFinished / WorkerCompleted
    Graph->>Graph: persist node and graph transition
    alt every node is done
        Graph-->>Waitlist: WorkflowCompleted
        Waitlist-->>Studio: WaitlistUpdated
    else worker or persistence fails
        Graph-->>Waitlist: WorkflowFailed or RuntimePersistenceFailed
        Waitlist-->>Studio: WaitlistUpdated
    end
```

Compilation uses the same worker protocol and lifecycle as a task workflow. Provider selection and bounded retries occur inside the dispatcher. Event names in this diagram are defined by the current event-taxonomy specification.
