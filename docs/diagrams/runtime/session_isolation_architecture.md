# Session Isolation & Pessimistic File Locking Architecture

This diagram illustrates how `forge.exe` safely manages two completely isolated executing sessions (`exec-001` and `exec-002`) that are both attempting to modify the same shared physical file (`src/main.go`).

```mermaid
sequenceDiagram
    participant User
    participant Bus as Go Event Bus
    participant S1 as Session 1 (exec-001)
    participant S2 as Session 2 (exec-002)
    participant Mutex as File Mutex Map
    participant Disk as Physical File System

    User->>Bus: Queue Prompt A (Refactor Auth)
    User->>Bus: Queue Prompt B (Add rate limits)

    Bus->>S1: Spawn Workflow (Namespace: exec-001)
    Bus->>S2: Spawn Workflow (Namespace: exec-002)

    Note over S1, S2: Both agents determine they need to edit `src/main.go`

    S1->>Mutex: FileLockRequested (src/main.go)
    Mutex-->>S1: Lock Acquired
    S1->>Disk: Read fresh state of src/main.go
    
    S2->>Mutex: FileLockRequested (src/main.go)
    Note over S2, Mutex: Agent B execution Paused (Blocked by Mutex)

    S1->>Disk: Write modifications to src/main.go
    S1->>Mutex: Release Lock (src/main.go)

    Mutex-->>S2: Lock Acquired
    Note over S2: Agent B Resumes Execution
    S2->>Disk: Read *fresh* state of src/main.go (Contains Agent A's edits)
    S2->>Disk: Write modifications to src/main.go
    S2->>Mutex: Release Lock (src/main.go)

    Note over Disk: Final state contains both features without corruption or merge conflicts.
```
