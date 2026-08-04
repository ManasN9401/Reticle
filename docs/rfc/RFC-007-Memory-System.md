# RFC-007 — Memory System

Status: Stable
Version: 1.0.0
Author: HyperParallel Core
Last Updated: 2026-08-03

---

## 1. Purpose
This document defines every layer of the framework's memory. It establishes how state is persisted, shared, and versioned across the lifecycle of agents and workflows.

## 2. Motivation
HyperParallel agents are modeled as pure, deterministic functions (Worker Protocol v1). They do not retain persistent dynamic memory across invocations. To orchestrate state between these isolated, ephemeral processes, the runtime must provide a centralized, externally managed Memory System capable of handling both large documents (Artifacts) and rapid scalar variables (Shared Memory).

## 3. Scope
This RFC covers:
- The Append-Only Artifact Store (for large, formal data objects).
- Artifact provenance and versioning.
- The Shared Runtime Memory (for scratchpad variables and flags).
- Scoping rules for Shared Memory.
- The lifecycle of state injection and extraction.

## 4. Philosophy
Memory should be kept strictly out of the sandbox. The runtime is the authoritative source of state, and agents should operate only on data explicitly injected into their context.

## 5. Principles
- **Immutability (Artifacts)**: Artifacts cannot be mutated or deleted. Updates trigger a new, monotonically increasing Version.
- **Traceability**: Every artifact must retain immutable lineage (who produced it and what parents were read to produce it).
- **Hierarchical Isolation (Shared Memory)**: Shared state must be isolated via explicit scopes (`global`, `workflow`, `agent`) to prevent data leakage and side effects.

## 6. Architectural Laws
1. The Orchestrator must intercept required dependencies and proactively inject them into the `Task` JSON sent over `stdin`. Agents do not query memory directly.
2. Agents must output memory mutations (e.g., `MemoryWriteRequested`, `ArtifactsProduced`) to `stdout`.
3. The Artifact Store must guarantee append-only semantics, enabling the retrieval of historical states via `GetVersion(id, version)`.

## 7. Rationale
By keeping state out of the sandbox and relying on the Orchestrator to inject/extract state via standard input/output, we preserve the deterministic purity of the workers. This makes the system trivially scalable and future-proofs it for seamless migrations (e.g., replacing standard streams with WebSockets, gRPC, or MCPs).

## 8. Trade-offs
- Passing all state via JSON payloads can become a bottleneck if artifacts are extremely large (e.g., gigabytes of data). Future revisions may need to introduce pass-by-reference (e.g., file paths) for massive payloads.

## 9. Future Considerations
- Introducing Long-Term Memory (vector databases) for semantic retrieval.
- Implementing Federated Memory Systems to share state across multiple physical Orchestrator instances.

## 10. References
- Deprecated Memory Spec: `docs/specifications/runtime-memory/v1/001-memory.md`

## 11. Related RFCs
- RFC-003 — Runtime
- RFC-008 — Agent Architecture
