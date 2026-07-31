---
Status: Stable
Author: HyperParallel Core
Date: 2026-07-31
---

# Workflow Definition Schema (v1)

HyperParallel workflows define a static Directed Acyclic Graph (DAG) representing the sequence of agent execution.

## Schema

```yaml
id: string          # Required: A globally unique identifier for this workflow.
name: string        # Required: Human-readable name.
version: string     # Required: Semantic version of this workflow.

nodes:              # Required: A list of executable nodes in the graph.
  - id: string      # Required: The unique node ID.
    agent: string   # Required: The registered Agent ID this node executes.

edges:              # Optional: A list of dependencies between nodes.
  - from: string    # Required: The producer Node ID.
    to: string      # Required: The consumer Node ID.
```

## Validation Rules
During load time, the Orchestrator will rigorously validate the YAML for:
- Duplicate node IDs.
- Unregistered or missing agents.
- Edges that reference non-existent node IDs.
- Cycles (the graph must be strictly Acyclic).

## Example
```yaml
id: readme-generator
name: README Generator
version: 1.0.0

nodes:
  - id: outline
    agent: outline-gen
  - id: wording
    agent: wording-imp

edges:
  - from: outline
    to: wording
```
