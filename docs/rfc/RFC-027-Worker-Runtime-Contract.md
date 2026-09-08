> Implementation update (2026-09-08): this historical proposal is superseded where it conflicts with RFC-043 and the current specifications under docs/specifications/. See the audit repair ledger for remaining capability limits.

# RFC-027 — Worker Runtime Contract (v1)

Status: Stable
Version: 1.0.0
Author: Reticle Core
Last Updated: 2026-08-05

---

## 1. Purpose
This document strictly defines the JSON schema for the **Worker Runtime Contract**. It outlines exactly what a worker receives on standard input (`stdin`) and what it is expected to output on standard output (`stdout`) to successfully interact with the Orchestrator.

## 2. Motivation
Agents are entirely ephemeral and language-agnostic. They communicate with the Go-based Orchestrator strictly via `stdin` and `stdout`. A formal schema definition prevents architectural drift and explicitly documents how features like Shared Memory and Graph Mutations traverse the polyglot boundary.

## 3. Scope
This RFC covers:
- The JSON schema of the `Task` object injected via `stdin`.
- The JSON schema of the `TaskResponse` object expected via `stdout`.
- Structured `MemoryMutation` integration.

## 4. Architectural Laws
1. The Orchestrator will pipe exactly **one JSON object** to the worker process on boot.
2. The Worker must read this object until `EOF`.
3. Upon success, the Worker must emit exactly **one JSON object** to `stdout`, and exit with code `0`. (Note: RFC-026 enforces graceful fallback for noisy standard output streams, but emitting valid JSON remains the contract).

## 5. Stdin Contract: `Task` Payload
When an agent starts, it receives a payload conforming to the following structure:

```json
{
  "id": "exec-123|node-1",
  "agent_id": "llm-worker",
  "execution": "exec-123",
  "workflow": "startup-launch",
  "inputs": [
    {
      "artifact_id": "exec-123|prev-node_output",
      "version": 1,
      "name": "Previous Output",
      "data": "..."
    }
  ],
  "parameters": {
    "system_prompt": "You are a specialized agent.",
    "llm_model": "groq/llama-3.1-8b-instant"
  },
  "memory": {
    "global_api_token": "sk-12345",
    "workflow_theme": "dark"
  },
  "instructions": [
    "Follow standard company guidelines."
  ]
}
```

### Fields:
- **`inputs`**: Array of Artifact dependencies from upstream DAG nodes.
- **`parameters`**: Key-Value mapping of node-specific config (e.g. LLM routing, user prompts).
- **`memory`**: Key-Value mapping of required Shared Memory keys fetched proactively by the Orchestrator based on the Agent's YAML definition.
- **`instructions`**: Array of strings injected from the Global Instruction Store.

## 6. Stdout Contract: `TaskResponse` Payload
When an agent finishes, it must return a JSON payload:

```json
{
  "id": "exec-123|node-1",
  "artifact": {
    "id": "exec-123|node-1_output",
    "name": "Node 1 Output",
    "data": "The final processed content."
  },
  "memory": [
    {
      "key": "user_name",
      "value": "Alice",
      "scope": "workflow"
    }
  ],
  "graph_mutation": {
    "action": "delegate",
    "target_agent": "reviewer-worker",
    "return_to_supervisor": true
  }
}
```

### Fields:
- **`artifact` (optional)**: The primary formal output of the node. Downstream nodes depend on this. If omitted, the node simply completes without yielding data.
- **`memory` (optional)**: An array of `MemoryMutation` objects to write to the Shared Runtime Memory store. Valid scopes: `global`, `workflow`, `execution`, `agent`. (Defaults to `execution`).
- **`graph_mutation` (optional)**: Instructs the Orchestrator to dynamically rewrite the DAG at runtime (e.g. for dynamic delegation loops).

## 7. Rationale
By formally standardizing the Memory and Graph Mutation schemas as strictly-typed arrays/objects, we avoid string parsing errors and ambiguous prefixes. The agent knows exactly what to expect, and the orchestrator knows exactly how to parse it.

## 8. Related RFCs
- RFC-007 — Memory System
- RFC-008 — Agent Architecture
- RFC-026 — Worker Stdout Protocol
