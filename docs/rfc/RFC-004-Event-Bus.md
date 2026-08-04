# RFC-004 — Event Bus

Status: Stable
Version: 1.0.0
Author: HyperParallel Core
Last Updated: 2026-08-03

---

## 1. Purpose
This document defines the canonical list of domain events emitted and consumed by the HyperParallel framework components. It serves as the authoritative taxonomy for all event-driven communications within the runtime.

## 2. Motivation
In a completely decoupled, "Shared-Nothing, Event-Everything" architecture, the Event Bus is the central nervous system. Without a strictly governed taxonomy of events, components could drift out of sync, emit malformed data, or misunderstand event semantics, leading to catastrophic runtime failures.

## 3. Scope
This RFC covers:
- System lifecycle events.
- Workflow execution events.
- Automation trigger events.
- Dispatcher and Worker lifecycle events.
- Memory and State mutation events.

## 4. Philosophy
Events must be definitive. They represent things that *have happened*, not things that *might happen*. Naming conventions must reflect past-tense completion (e.g., `TaskCreated`, `WorkerStarted`, `WorkflowCompleted`).

## 5. Principles
- **Immutability**: Once an event is emitted, its payload cannot be altered.
- **Structured Payloads**: All event payloads must be rigorously typed or strictly conform to standard JSON structures.
- **Traceability**: Every event should carry enough context (Execution ID, Workflow ID, Task ID) to be correlated back to its source.

## 6. Architectural Laws
1. No component may bypass the Event Bus to trigger a state change in another component.
2. Event schema changes must be versioned and backward-compatible unless a major version bump occurs.
3. The framework must not crash if it receives an unrecognized event type; it should log and ignore it.

## 7. Rationale
By strictly enumerating the taxonomy here, developers building new subsystems (like a UI dashboard or a custom logger) can reliably subscribe to the bus knowing exactly what events will flow through it and what payloads to expect.

## 8. Trade-offs
- The requirement to keep documentation strictly in sync with code definitions of events introduces slight friction during rapid prototyping.

## 9. Future Considerations
- Introducing an `EventReplay` mechanism to allow debugging of failed executions by replaying the exact sequence of events.
- Defining strongly-typed Protocol Buffers or JSON Schemas for the event payloads rather than relying solely on struct documentation.

## 10. References
- Deprecated Event Taxonomy: `docs/specifications/event-taxonomy/v1/001-events.md`

## 11. Related RFCs
- RFC-003 — Runtime
- RFC-007 — Memory System
