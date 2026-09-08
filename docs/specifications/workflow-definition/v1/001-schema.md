---
status: accepted
owner: Reticle Project
updated: 2026-09-08
---
# Workflow definitions

Use schemas/workflow.schema.json. The root object has id, nodes and edges; there is no workflow wrapper. Each node has id, agent, optional parameters and modality. Edges use from/to. Nodes must be unique, agents available, endpoints valid and the graph acyclic. At most 128 nodes are admitted per execution. The compiler preserves parameters and modality.

Runtime definitions are copied per execution. Dynamic additions are execution-local and bounded. WorkflowCompleted and WorkflowFailed are terminal outcomes. Pausing stops new dispatch; already-running commands continue until completion or cancellation.
