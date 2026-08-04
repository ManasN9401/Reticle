# Architectural Backlog

This document records architectural ideas that have broad agreement but have not
yet been incorporated into permanent documentation.

The purpose is to preserve design intent while avoiding unnecessary delays in
writing formal documents.

---

## RFC-000

### Reasoned Evolution

The project should encourage architectural evolution based on evidence rather
than preserving existing designs for their own sake.

Architectural values should remain relatively stable.

Architectural mechanisms are expected to evolve.

Future RFCs should justify significant architectural changes by explaining:

- the problem being solved,
- why the current approach is insufficient,
- expected benefits,
- trade-offs.

Status: Agreed

---

## External Integrations

### MCP and API Support

While the Skeleton Runtime currently uses simple local scripts, the architecture must support true AI agents (LLMs) and Model Context Protocol (MCP) servers. The standard JSON-RPC STDIO IPC mechanism guarantees that any agent, whether a simple python script or a massive LLM acting through an MCP server, can be seamlessly plugged into the runtime. 

Status: Planned for future milestones.

---

## Dynamic Workflow Compilation (Zero-Boilerplate)

### Natural Language to Execution

Currently, each test workflow requires a bespoke `main.go` to bootstrap the `GraphEngine` and hardcoded YAML definitions. 
The end goal is a **Zero-Boilerplate Orchestrator**. 

The runtime will feature a top-level "Compiler Agent". When a user provides a natural language prompt (e.g., "Write a book about Space"), the Compiler Agent will:
1. Dynamically design the required Directed Acyclic Graph (DAG) of specialized agents.
2. Generate the temporary YAML workflow and agent definitions in memory.
3. Pass the generated DAG directly to the `GraphEngine` for execution.

This removes the need for compiling a unique `main.go` per task. The orchestrator becomes a universal CLI/API that takes a prompt and handles the rest.

Status: Planned for future milestones.

---

## Intelligent Model Routing (RFC-012)

### Dynamic Capability Matching

Currently, the model router defaults to a hardcoded `groq/llama-3.1-8b-instant` model defined in the agent's YAML. 

In the final architecture, the Model Router (as defined in RFC-012) will be deeply intelligent. Instead of relying on static definitions, it will route tasks dynamically to a fleet of available models (e.g., Claude 3.5 Sonnet for complex coding, GPT-4o for broad reasoning, Gemini 1.5 Pro for massive context windows, or local models for privacy/cost-saving). 

Routing decisions will be based on:
- Task complexity and required capabilities (vision, coding, reasoning).
- Context window requirements.
- Cost and latency constraints defined by the user's session.

Status: Planned for future milestones.