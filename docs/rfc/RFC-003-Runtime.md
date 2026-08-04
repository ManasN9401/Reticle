# RFC-003 — Runtime

Status: Stable
Version: 1.0.0
Author: HyperParallel Core
Last Updated: 2026-08-03

---

## 1. Purpose
This document defines the architecture and design philosophy of the HyperParallel Runtime Orchestrator. It serves as the blueprint for how the orchestrator manages execution lifecycles, event communication, and component registration.

## 2. Motivation
Traditional workflow engines often tightly couple the orchestration logic with the execution logic, leading to rigid, hard-to-maintain systems. HyperParallel requires an orchestrator capable of coordinating thousands of concurrent, highly-isolated worker processes dynamically without creating massive dependency graphs.

## 3. Scope
This RFC covers:
- The overall architectural philosophy of the Orchestrator.
- The high-level Event Bus routing mechanism.
- The three primary subsystems: Workflow Engine, Dispatcher, and Subscription Manager.
- Execution paradigms (Workflow vs. Automation).

## 4. Philosophy
The Orchestrator strictly follows a **Shared-Nothing, Event-Everything** philosophy. Internal components do not hold direct references to one another. Instead, they communicate exclusively by publishing and subscribing to standardized `RuntimeEvents` over the central `events.Bus`.

## 5. Principles
- **Decoupling**: Adding new subsystems must require zero changes to existing orchestration logic.
- **Asynchronicity**: Core execution must never block on event handling unless explicitly required for ordering.
- **Isolation**: Physical execution (Worker processes) must be strictly isolated from logical orchestration.

## 6. Architectural Laws
1. The Orchestrator core must not directly invoke worker processes; it must emit `TaskCreated` events.
2. The Dispatcher must not determine *when* to run a task, only *how* to run it when instructed by a `TaskCreated` event.
3. All subsystems must rely on the Event Bus as the single source of truth for runtime state transitions.

## 7. Rationale
By enforcing strict event-driven decoupling:
- We can seamlessly introduce dynamic sub-graphs (Supervisor agents) that inject mutations via the event bus without breaking the main DAG execution.
- Observability and auditing become trivial, as every state change is already a standardized event flowing through a central pipe.

## 8. Trade-offs
- **Complexity**: Debugging event-driven systems can be challenging because execution traces are non-linear.
- **Overhead**: Serializing and deserializing payloads across the bus adds minor latency compared to direct function calls. This is acceptable for orchestration.

## 9. Future Considerations
Future versions may need to support distributed execution across multiple physical hosts. The event-driven design naturally lends itself to this, as the internal memory bus can be replaced with a distributed message broker (e.g., Redis, Kafka) with minimal architectural changes.

## 10. References
- Original Architecture Spec (Deprecated): `docs/specifications/orchestrator-architecture/v1/001-architecture.md`

## 11. Related RFCs
- RFC-004 — Event Bus
- RFC-008 — Agent Architecture
