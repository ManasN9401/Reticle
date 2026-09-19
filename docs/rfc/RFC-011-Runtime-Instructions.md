---
status: historical
owner: Reticle Project
updated: 2026-09-19
---

> Historical design record. Current versioned specifications and schemas take precedence.

# RFC-011 — Runtime Instructions

Original status: Stable
Version: 1.0.0
Author: Reticle Core
Original last updated: 2026-08-03

---

## 1. Purpose
This document defines how the Reticle framework supports dynamic behavioral modification at runtime via "Runtime Instructions."

## 2. Motivation
Agents are defined by their YAML and backing code. However, users often need to inject temporary or persistent stylistic overrides (e.g., "Use tabs instead of spaces," "Do not use React classes") without having to hardcode these into the agent's core identity. The system needs a flexible way to overlay human instructions onto agents dynamically.

## 3. Scope
This RFC covers:
- The Instruction Store.
- The three target scopes (Global, Workflow, Agent).
- The injection mechanism via the Dispatcher.
- Modification of the Worker Protocol JSON payload.

## 4. Philosophy
Instructions are supplemental context. The Orchestrator resolves and orders them, and the Worker is responsible for integrating them into its operational context (such as an LLM System Prompt).

## 5. Principles
- **Scoping**: Instructions can be broadly applied or narrowly targeted.
- **Priority via Ordering**: More specific instructions should take precedence over broader ones.
- **Dynamic**: Instructions can be injected, updated, or removed from the `InstructionStore` at runtime without restarting the orchestrator.

## 6. Architectural Laws
1. Instructions must be managed by a thread-safe `InstructionStore`.
2. When the `Dispatcher` intercepts a `TaskCreated` event, it must query the store and attach the resolved instructions to the `Task.Instructions` array.
3. Instructions must be aggregated and appended in a strict priority order: **Global -> Workflow -> Agent**. This typically gives the Agent-scoped instruction the "last word" in an LLM context window.

## 7. Rationale
Injecting these instructions into the Task JSON via the Dispatcher ensures the workers remain stateless and decoupled from the Orchestrator's internal memory bus.

## 8. Trade-offs
- Ordering (Global -> Workflow -> Agent) is a heuristic. It assumes the underlying worker processes instructions sequentially and prioritizes the end of the list. If a worker uses a different priority algorithm, conflicts could arise.

## 9. Future Considerations
- Allowing Supervisors to dynamically emit `MemoryWriteRequested` events that target the `InstructionStore`, enabling agents to modify instructions for other agents at runtime.

## 10. References
- Deprecated Instructions Spec: `docs/specifications/runtime-instructions/v1/001-instructions.md`

## 11. Related RFCs
- RFC-003 — Runtime
- RFC-008 — Agent Architecture
