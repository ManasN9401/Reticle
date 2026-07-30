---
title: 0001 - Skeleton Supervisor Hardcoding and Technical Debt
document_type: ADR
authority: Informative
status: Accepted
version: 1.0.0
scope: Runtime
stability: Stable
owner: HyperParallel Project
---

# 0001 - Skeleton Supervisor Hardcoding and Technical Debt

## Context
During the Skeleton Runtime phase, the primary objective was to validate that language-agnostic workers could be orchestrated using a shared Go runtime via JSON-RPC over STDIO. To achieve this quickly, the task parsing and dynamic task graph management features of the Supervisor were bypassed. 

## Decision
We decided to explicitly hardcode the task assignments in the `main.go` test script (e.g., manually sequencing `AssignTask` calls) rather than implementing a dynamic DAG parser for the `task.schema.json`.

## Consequences
- **Positive:** We successfully validated the event bus, shared memory, and cross-process JSON-RPC communication without being blocked by complex graph traversal logic.
- **Negative (Technical Debt):** The current `Supervisor` is extremely "dumb". It does not read task schemas, does not dynamically resolve dependencies, and cannot automatically retry or fan-out workloads.

## Remediation Plan
This technical debt is recorded here to ensure it is not forgotten. In the next major iteration (Milestone 2/3), the Supervisor must be upgraded to dynamically parse task graphs and manage dependencies autonomously.
