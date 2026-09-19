---
status: accepted
owner: Reticle Project
updated: 2026-09-19
---

# Session isolation architecture

```mermaid
flowchart LR
    Q[Waitlist item exec-id] --> M[Transactional execution memory]
    Q --> C[Compiler execution compile-exec-id]
    M --> C
    C --> D[Generated agents and workflow]
    D --> R[Runtime execution exec-id]
    R --> S[.reticle/sessions/exec-id]
    S --> A[agents/]
    S --> W[workflows/]
    S --> F[src and artifacts]
    R --> E[Durable execution snapshot]
    R --> X[Durable external-effect ledger]
```

Each waitlist item owns an isolated session directory. Concurrent sessions do not edit one shared physical `src` tree, and the v1 worker protocol does not support the legacy stdin file-lock message scheme. File tools enforce containment inside the selected session and use exact, guarded mutations. Follow-on work can explicitly inherit checked files from an earlier execution in the same sequential group.

Isolation prevents accidental cross-run writes. It does not merge two completed sessions back into an external project; that requires an explicit review/integration step.
