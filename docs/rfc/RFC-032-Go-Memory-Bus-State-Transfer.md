---
status: historical
owner: Reticle Project
updated: 2026-09-19
---

> Historical design record. Current versioned specifications and schemas take precedence.

> Implementation update (2026-09-08): this historical proposal is superseded where it conflicts with RFC-043 and the current specifications under docs/specifications/. See the audit repair ledger for remaining capability limits.

# RFC-032: Go Memory Bus & State Transfer

## Status
Accepted

## Context
Reticle features a bifurcated architecture: the orchestrator and WebSocket engine are written in high-performance Go (`forge.exe`), while the intelligent agents and workflow definitions are executed via Python subprocesses. The orchestrator needs a robust way to pass state (user prompts, active workspace paths, environment variables) to the Python workers without relying on hardcoded file paths or fragile CLI arguments.

## Proposal
Leverage a generic JSON-based Memory Event Bus.
1. `forge.exe` publishes a `MemoryWriteRequested` event to the `EventBus` when initializing a workspace.
2. The Go `MemoryStore` captures this and holds it in the global state.
3. When `executor.go` spawns a Python worker, it dumps the entire current `MemoryStore` state into a JSON payload and injects it via `stdin`.
4. The Python worker parses `sys.stdin.readline()`, extracting `req.get("memory", {})`, allowing it to dynamically resolve paths like `workspace_dir`.

## Consequences
- **Pros:** Highly decoupled. Workers are stateless and purely functional based on the `stdin` payload.
- **Cons:** Large memory payloads can slow down `stdin` ingestion if the global context grows too large (e.g., hundreds of files).

## Implementation
Implemented in `main.go` and consumed by `architect.py`, `coder.py`, and `scaffolder.py`.
