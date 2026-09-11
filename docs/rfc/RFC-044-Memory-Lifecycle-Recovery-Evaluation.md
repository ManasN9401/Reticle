---
Status: Accepted
Author: Reticle memory lifecycle repair
Date: 2026-09-09
---

# RFC-044: Memory lifecycle, recovery and evaluation

Reticle keeps short-lived task facts in scoped runtime memory and final outputs in versioned artifacts. The runtime applies these rules:

- Execution-scoped facts expire after 24 hours by default and are released when their workflow completes, fails or is killed. Global, workflow and agent scopes require deliberate lifecycle management.
- Runtime memory is limited to 10,000 entries and 16 MiB by default. A rejected write emits `MemoryWriteRejected`; workers wait for acceptance before reporting completion. All memory mutations and the optional final artifact from one worker response commit atomically or roll back together.
- Every shared-memory key has a monotonically increasing revision. `expected_version` provides compare-and-set behavior: zero creates only if absent, and a positive value updates only that revision. Omitting it retains unconditional last-write-wins compatibility.
- Artifact histories retain the latest 100 versions by default while logical version numbers continue increasing. Final artifacts are retained when execution scratch memory is released.
- When `RETICLE_ROOT` is set, the orchestrator synchronously saves a restricted, versioned snapshot at `.reticle/memory/state.json` after accepted mutations and lifecycle cleanup. A temporary file is synced before replacement. Startup restores valid, unexpired state. Unsupported or corrupt snapshots disable recovery rather than silently resetting the file.
- Persistence failure rolls back the in-process mutation and is reported. The requesting worker receives failure. Snapshot replacement protects the prior file, but this is single-process local durability rather than a replicated transactional database.

Configuration uses `RETICLE_MEMORY_MAX_ENTRIES`, `RETICLE_MEMORY_MAX_MIB`, `RETICLE_EXECUTION_MEMORY_TTL_HOURS`, `RETICLE_ARTIFACT_MAX_VERSIONS`, and `RETICLE_MEMORY_PERSISTENCE=false` to disable persistence. Invalid or non-positive values fall back to defaults.

The memory quality evaluation under `evals/memory-quality` compares no-memory, full-context and selected-memory conditions. Offline mode validates selection and prompt contracts without network use. A `--model` run uses the configured LiteLLM provider, records exact-answer accuracy and may incur provider cost. Results must name the model and attempt count; an offline pass is not evidence of model reasoning quality.

Known limits: snapshots rewrite the bounded retained state for every accepted mutation, so very write-heavy workloads need a journal or database backend. Shared memory remains snapshot-at-dispatch rather than live subscription. Cross-process writers are unsupported. Quality cases are deliberately small and should grow from real failure examples.
