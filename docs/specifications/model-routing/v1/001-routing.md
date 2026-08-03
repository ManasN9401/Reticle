---
Status: Draft
Author: HyperParallel Core
Date: 2026-08-03
---

# Model Routing Architecture (v1)

As part of the Intelligence Layer (Phase 3), the framework requires a dynamic approach to model selection. Instead of statically hardcoding a frontier API model (like GPT-5.5) or a specific local model for every agent, the runtime will implement an intelligent router.

## 1. Dynamic Bayesian Utility Estimates

Instead of assigning every model a fixed abstract "cost" or hardcoding rules, the model router maintains a **Bayesian estimate of expected utility** for different classes of tasks.

The router continuously learns from experience (e.g., whether a worker successfully completed a task, required retries, or failed) and updates its success probability matrix.

### Example Utility Matrix

| Task Type | Local 32B | Local 72B | GPT-5.5 |
| :--- | :--- | :--- | :--- |
| **Refactoring** | 0.96 | 0.98 | 0.99 |
| **API design** | 0.81 | 0.90 | 0.98 |
| **Regex** | 0.99 | 0.99 | 0.99 |
| **SQL** | 0.94 | 0.96 | 0.97 |
| **Multi-file debugging**| 0.68 | 0.84 | 0.97 |

## 2. Threshold-Based Selection

These utility values are not hardcoded. They are updated dynamically from the runtime's historical experience and telemetry (e.g. `WorkerCompleted` vs `WorkerFailed`).

When a supervisor or the runtime needs to dispatch a task, it evaluates the **Threshold Policy**:

> **Policy:** The router chooses the *cheapest* model whose expected success rate exceeds the required confidence threshold.

**Example Scenario:**
If the system is running a "Refactoring" task and requires a 95% success confidence threshold, the router will inspect the utility matrix:
- GPT-5.5 (0.99) - Exceeds threshold, high cost
- Local 72B (0.98) - Exceeds threshold, medium cost
- Local 32B (0.96) - Exceeds threshold, low cost

Because Local 32B has demonstrated a 96% success rate on refactoring, there is no reason to pay for a frontier API model. The router will automatically dispatch the task to the Local 32B model, optimizing cost while guaranteeing the required success probability.
