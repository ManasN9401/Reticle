# RFC 036: Human-in-the-Loop & Concentrated Workflows

## 1. Overview
As Reticle scales to handle massively parallel agent workflows, the need for localized context ("concentrated workflows") and human intervention becomes critical. Blindly executing a 50-node Directed Acyclic Graph (DAG) limits user control and increases the risk of cascading hallucinations.

This RFC outlines three architectural patterns to inject granular control and logical concentration into the execution graph without violating the stateless, distributed nature of the engine.

## 2. Hierarchical DAGs (The "Lead Agent" Pattern)

### Problem
Forcing a single Architect to design and a single graph to manage 50+ nodes leads to a bloated, flat structure. 

### Proposed Solution
Instead of a single, massive flat graph, we allow agents to recursively act as Architects (Sub-Architects).
- **Lead Agents**: The top-level Architect creates "Lead Agents" (e.g., `frontend-lead-agent`, `backend-lead-agent`).
- **Dynamic Sub-Graphs**: When a Lead Agent executes, it doesn't output code. Instead, its artifact is a *new* DAG JSON payload. 
- **Orchestrator Support**: The `GraphEngine` must detect when an artifact is a sub-graph and seamlessly inject those nodes into the running execution state, resolving them before marking the Lead Agent as completed.

This concentrates logic logically: all frontend context is handled by the `frontend-lead-agent` and its immediate children, reducing cross-contamination in the global memory space.

## 3. Interactive DAG Approval (The "Blueprint Phase")

### Problem
Users currently submit a prompt, and the Architect instantly commits to a DAG structure. If the Architect misunderstands the prompt, the entire execution wastes tokens.

### Proposed Solution
Split the compilation phase into a two-step process in the UI:
1. **Blueprint Generation**: The Architect generates the DAG JSON. Instead of immediately spawning prompt engineers and passing it to the `GraphEngine`, the engine halts and pushes a `WaitlistStateRequested` with a new `StatusPendingReview` status.
2. **Interactive Canvas**: The UI renders the DAG visually using a library like React Flow. The user can add nodes, delete unnecessary branches, or rewrite the Architect's initial assignments.
3. **Commit**: The user hits "Approve Blueprint", which sends a WebSocket command to resume the compilation, generating the final system prompts and launching the graph.

## 4. Explicit Human Checkpoint Nodes

### Problem
Certain destructive or critical actions (e.g., executing deployment scripts, merging to `main`, dropping databases) should not happen automatically, even within an approved DAG.

### Proposed Solution
Introduce a native `human-review-agent` node type.
- **Node Injection**: The Architect can be instructed to place a `human-review-agent` immediately before any critical `action-agent`.
- **Runtime Suspension**: When the `GraphEngine` schedules a `human-review-agent`, the worker suspends execution and fires a `HumanReviewRequested` event on the bus.
- **UI Resolution**: The telemetry UI displays the accumulated artifacts of the parent nodes. The user can review the files and click "Approve" or "Reject".
- **Resumption**: The UI fires a `HumanReviewResolved` event back over the WebSocket. If approved, the node completes successfully and downstream execution continues. If rejected, the node fails, halting that branch of the DAG.
