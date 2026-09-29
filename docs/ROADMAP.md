---
status: active
owner: Reticle Project
updated: 2026-09-29
---

# Reticle roadmap and RFC register

## Authority

This document records direction and indexes filed RFCs. It is not an
implementation specification. Current versioned documents under
`docs/specifications/` and their schemas take precedence, followed by accepted
RFCs. A `historical` RFC is design evidence only; `draft` means proposed, not
implemented or approved.

RFC identifiers belong only to files actually filed under `docs/rfc/`. Earlier
versions of this roadmap assigned RFC-like numbers to prospective topics. Those
labels were placeholders and are not retained as a second numbering system.

## Current course of action

The extensibility sequence is now implemented:

1. [RFC-046](rfc/RFC-046-MCP-Integration.md): local stdio MCP is the first
   external broker adapter, with authenticated runtime APIs and Studio management.
2. [RFC-047](rfc/RFC-047-Plugin-System.md): validated local bundles contribute
   agents, skills, brokered tools and MCP declarations through existing registries.

[RFC-050](rfc/RFC-050-Attempt-Scoped-Tool-Broker.md) is accepted and its broker,
dispatcher/worker integration and fixture adapter contract are implemented.

The recommended next step is an acceptance and hardening cycle: exercise signed
or checksummed provenance options, crash recovery, invalid bundle upgrades and a
broader MCP fixture matrix before proposing remote transports or a marketplace.
After that, RFC-049 can add memory inspection without expanding execution
authority. RFC-048 remains blocked on genuine process isolation.

[RFC-049](rfc/RFC-049-Memory-Query-Management.md) is an independent proposed
Studio/runtime surface and may be scheduled separately after review.
[RFC-048](rfc/RFC-048-Meta-Scaffolder-Revised.md) records preconditions only;
it does not authorize implementation of autonomous self-modification.

The detailed current planning snapshot is
[Project State — 27 September 2026](planning/PROJECT_STATE_27-09-26.md).

## Filed RFC register

Statuses below are copied from each file's YAML frontmatter. This table makes no
additional claim about implementation completeness.

| RFC | Title | Status |
| --- | --- | --- |
| [RFC-001](rfc/RFC-001-Terminology.md) | Terminology | `historical` |
| [RFC-002](rfc/RFC-002-System-Overview.md) | System Overview | `historical` |
| [RFC-003](rfc/RFC-003-Runtime.md) | Runtime | `historical` |
| [RFC-004](rfc/RFC-004-Event-Bus.md) | Event Bus | `historical` |
| [RFC-007](rfc/RFC-007-Memory-System.md) | Memory System | `historical` |
| [RFC-008](rfc/RFC-008-Agent-Architecture.md) | Agent Architecture | `historical` |
| [RFC-009](rfc/RFC-009-Skills.md) | Skills | `historical` |
| [RFC-010](rfc/RFC-010-Supervisor-Graph.md) | Supervisor Graph | `historical` |
| [RFC-011](rfc/RFC-011-Runtime-Instructions.md) | Runtime Instructions | `historical` |
| [RFC-012](rfc/RFC-012-Model-Routing.md) | Model Routing | `historical` |
| [RFC-022](rfc/RFC-022-Dashboard-Graph-UI.md) | Dashboard and Graph UI | `historical` |
| [RFC-025](rfc/RFC-025-Dynamic-Workflow-Compilation.md) | Dynamic Workflow Compilation | `historical` |
| [RFC-026](rfc/RFC-026-Worker-Stdout-Protocol.md) | Worker Fault Tolerance and Stdout Protocol | `historical` |
| [RFC-027](rfc/RFC-027-Worker-Runtime-Contract.md) | Worker Runtime Contract v1 | `historical` |
| [RFC-028](rfc/RFC-028-API-Rate-Limit-Load-Balancing.md) | API Rate Limit Load Balancing | `historical` |
| [RFC-029](rfc/RFC-029-Python-Tenacity-Retry-Policy.md) | Python Tenacity Retry Policy and Logging Suppression | `historical` |
| [RFC-030](rfc/RFC-030-Local-LLM-Integration-via-VPS.md) | Local LLM Integration via VPS | `historical` |
| [RFC-031](rfc/RFC-031-Artifact-Outputs.md) | Artifact Outputs Standard | `historical` |
| [RFC-032](rfc/RFC-032-Go-Memory-Bus-State-Transfer.md) | Go Memory Bus and State Transfer | `historical` |
| [RFC-033](rfc/RFC-033-Isolated-Session-Workspaces.md) | Isolated Session Workspaces | `historical` |
| [RFC-034](rfc/RFC-034-Complex-Agentic-Workflows.md) | Complex Agentic Workflows via ReAct | `historical` |
| [RFC-035](rfc/RFC-035-Agent-Evolution-Meta-Scaffolder.md) | Agent Evolution and Meta-Scaffolder | `historical` |
| [RFC-036](rfc/RFC-036-Human-in-the-Loop-Workflows.md) | Human-in-the-Loop and Concentrated Workflows | `historical` |
| [RFC-037](rfc/RFC-037-Dynamic-Context-Compression.md) | Dynamic Context Compression and Local RAG | `historical` |
| [RFC-038](rfc/RFC-038-HitL-Checkpoints.md) | Human-in-the-Loop Checkpoints | `historical` |
| [RFC-039](rfc/RFC-039-Smart-Predictive-Rate-Limiting.md) | Smart Predictive Rate Limiting | `historical` |
| [RFC-040](rfc/RFC-040-Bayesian-Model-Routing-Matrix.md) | Bayesian Model Routing Matrix and Fallback Logic | `historical` |
| [RFC-041](rfc/RFC-041-Agent-Execution-Lifecycle-Control.md) | Agent Execution Lifecycle Control | `historical` |
| [RFC-042](rfc/RFC-042-Native-ComfyUI-Integration.md) | Native ComfyUI Integration | `historical` |
| [RFC-043](rfc/RFC-043-Audit-Contract-Repairs.md) | Repair Existing Runtime Contracts | `accepted` |
| [RFC-044](rfc/RFC-044-Memory-Lifecycle-Recovery-Evaluation.md) | Memory Lifecycle, Recovery and Evaluation | `accepted` |
| [RFC-045](rfc/RFC-045-Durable-Execution-Capabilities-Effects.md) | Durable Execution, Capabilities and External Effects | `accepted` |
| [RFC-046](rfc/RFC-046-MCP-Integration.md) | MCP Integration | `accepted` |
| [RFC-047](rfc/RFC-047-Plugin-System.md) | Plugin System | `accepted` |
| [RFC-048](rfc/RFC-048-Meta-Scaffolder-Revised.md) | Meta-Scaffolder, Revised | `draft` |
| [RFC-049](rfc/RFC-049-Memory-Query-Management.md) | Memory Query and Management Surface | `draft` |
| [RFC-050](rfc/RFC-050-Attempt-Scoped-Tool-Broker.md) | Attempt-Scoped Tool Broker | `accepted` |

## Unfiled identifiers

There are no RFC files numbered 000, 005–006, 013–021 or 023–024. Some of
these numbers appeared as prospective labels in the old roadmap, but no RFC was
filed under them. They are gaps in the historical sequence, not missing links
and not aliases for later RFCs.

New proposals use the next unused filed identifier rather than filling a gap or
reusing an old placeholder. The process is defined in
[RFC_PROCESS.md](governance/RFC_PROCESS.md).

## Unnumbered future topics

The following remain ideas until individual RFCs are filed and reviewed:

- distributed execution and remote workers;
- cloud orchestration and external effect adapters;
- agent evaluation and reputation;
- simulation environments;
- a marketplace for agents, skills and plugins;
- visual workflow authoring;
- enterprise administration;
- multi-project orchestration;
- federated memory.

Listing a topic here does not reserve an RFC number or imply acceptance.
