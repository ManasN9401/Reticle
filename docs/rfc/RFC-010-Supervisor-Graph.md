# RFC-010 — Supervisor Graph

Status: Stable
Version: 1.0.0
Author: Reticle Core
Last Updated: 2026-08-03

---

## 1. Purpose
This document defines the graph-based orchestration model of Reticle, explaining how static workflows (Directed Acyclic Graphs) are executed and how Supervisor agents can dynamically mutate these graphs at runtime.

## 2. Motivation
While declarative, static DAGs are easy to reason about, they are insufficiently flexible for highly agentic workflows where an intelligence layer (Supervisor) needs to decide *at runtime* how to break down a task, who to assign it to, and when it is complete. We need a system that supports both static predictability and dynamic agentic delegation.

## 3. Scope
This RFC covers:
- The YAML Schema for static workflows (Nodes and Edges).
- The `GraphEngine` execution model.
- Supervisor nodes vs. Worker nodes.
- Dynamic graph construction (`GraphMutationRequested`).
- Delegation and Cyclical execution.

## 4. Philosophy
The core orchestrator executes a rigid, predictable DAG. True intelligence and cyclical routing are achieved by allowing Supervisor agents to inject temporary sub-graphs into the orchestrator's state at runtime.

## 5. Principles
- **Acyclic by Default**: The base workflow definition must be strictly Acyclic.
- **Controlled Cycles**: Cycles are only permitted through explicit Supervisor delegation (`GraphMutationRequested`).
- **Suspension**: When a Supervisor delegates a task, it is suspended. It is woken up via a new Task execution when the delegated sub-graph completes.

## 6. Architectural Laws
1. The Workflow YAML must contain `nodes` (agents) and `edges` (dependencies). The Orchestrator must validate this at load time and reject any cycles.
2. The `GraphEngine` must evaluate dependencies topologically.
3. If an agent emits a `GraphMutationRequested` event via stdout with `action: delegate`, the `GraphEngine` must inject a new transient node for the target agent and pause the Supervisor's progression in the DAG.
4. When the dynamically injected node completes, the `GraphEngine` must automatically create a new execution context for the Supervisor, injecting the child's artifact into the Supervisor's inputs.

## 7. Rationale
By building the dynamic mutation logic directly into the `GraphEngine`'s event loops, we allow supervisors to build complex, multi-agent hierarchies dynamically without the framework needing to know about them ahead of time.

## 8. Trade-offs
- Graph mutation introduces non-deterministic execution paths into an otherwise predictable system. The state of the graph becomes entirely dependent on LLM outputs (from the Supervisor).

## 9. Future Considerations
- Supporting parallel dynamic delegation (e.g., a Supervisor splitting a task among 5 agents simultaneously).
- Timeout and fallback policies for dynamically delegated nodes.

## 10. References
- Deprecated Workflow Spec: `docs/specifications/workflow-definition/v1/001-schema.md`

## 11. Related RFCs
- RFC-003 — Runtime
- RFC-008 — Agent Architecture
