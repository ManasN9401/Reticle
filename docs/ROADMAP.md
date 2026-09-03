# AI Orchestrator Framework Roadmap

> **Status:** Planning
>
> This document outlines the planned architecture documentation for the AI Orchestrator Framework. It serves as a high-level roadmap only and intentionally avoids implementation details. Each phase consists of one or more RFCs (Request for Comments) that will become the authoritative specifications for the framework.

---

# Documentation Philosophy

The framework is documented using RFCs.

Each RFC represents a single architectural topic and is intended to become the source of truth for that subsystem.

RFCs are designed to:

- Explain *why* a design decision exists.
- Define *what* the subsystem must accomplish.
- Establish architectural constraints.
- Provide implementation guidance without dictating specific technologies.
- Remain version controlled throughout the lifetime of the project.

The roadmap progresses from foundational concepts toward implementation-specific architecture.

---

# Phase 1 — Foundation

The foundation establishes the principles upon which every other subsystem will be built.

No implementation details are defined during this phase.

## RFC-000 — Philosophy

Defines:

- Project vision
- Long-term goals
- Core architectural principles
- Design philosophy
- Guiding constraints
- Non-goals
- Project values
- Framework identity

---

## RFC-001 — Terminology

Creates a shared language for the framework.

Defines every major concept including (but not limited to):

- Agent
- Skill
- Plugin
- Tool
- Supervisor
- Worker
- Task
- Job
- Event
- Memory
- Knowledge
- Context
- Runtime
- Capability
- Graph
- Session

Every future RFC references these definitions.

---

## RFC-002 — System Overview

Provides a high-level overview of the entire platform.

Introduces:

- Runtime architecture
- Core components
- High-level execution flow
- Agent ecosystem
- Plugin ecosystem
- Memory architecture
- Event-driven design
- Supervisor graph
- User interaction model

This document acts as the architectural "map" of the framework.

---

# Phase 2 — Core Runtime

This phase defines the runtime responsible for executing, coordinating, and monitoring all agents.

## RFC-003 — Runtime

Defines the runtime environment responsible for:

- Bootstrapping
- Lifecycle management
- Initialization
- Shutdown
- Runtime services
- Component registration

---

## RFC-004 — Event Bus

Defines the framework's event-driven architecture.

Includes:

- Event lifecycle
- Event routing
- Event subscriptions
- Event priorities
- Event persistence
- Event replay
- Internal communication

---

## RFC-005 — Scheduler

Defines how work is scheduled.

Topics include:

- Task scheduling
- Priorities
- Dependencies
- Resource allocation
- Parallel execution
- Fair scheduling
- Queue management

---

## RFC-006 — Task System

Defines:

- Task creation
- Assignment
- Ownership
- Dependencies
- Status
- Retry behaviour
- Cancellation
- Completion

---

## RFC-007 — Memory System

Defines every layer of framework memory.

Includes:

- Knowledge
- Context
- Project memory
- Runtime state
- Shared memory
- Long-term memory
- Session memory
- Retrieval
- Synchronization

---

# Phase 3 — Intelligence Layer

Defines how intelligence is represented inside the framework.

---

## RFC-008 — Agent Architecture

Defines:

- Agent lifecycle
- Agent identity
- Configuration
- Loading
- Registration
- Discovery
- Runtime behaviour
- Agent capabilities

---

## RFC-009 — Skills

Defines reusable capabilities.

Topics include:

- Skill composition
- Skill inheritance
- Skill discovery
- Skill versioning
- Skill reuse
- Skill dependencies

---

## RFC-010 — Supervisor Graph

Defines the graph-based orchestration model.

Topics include:

- Supervisor nodes
- Worker nodes
- Dynamic graph construction
- Graph mutation
- Temporary supervisors
- Delegation
- Team formation
- Coordination strategies

---

## RFC-011 — Runtime Instructions

Defines runtime modification.

Includes:

- Human instructions
- Universal directives
- Agent-specific directives
- Temporary overrides
- Persistent overrides
- Enable/disable functionality
- Live configuration updates

---

## RFC-012 — Model Routing

Defines model management.

Includes:

- Model selection
- Multi-model execution
- Cost optimisation
- Fallback behaviour
- Local models
- Cloud models
- Capability matching

---

# Phase 4 — Extensibility

Defines how the framework grows over time.

---

## RFC-013 — Plugin System

Defines:

- Plugin lifecycle
- Discovery
- Registration
- Hot loading
- Hot unloading
- Dependencies
- Version compatibility

---

## RFC-014 — MCP Integration

Defines interaction with Model Context Protocol servers.

Topics include:

- Discovery
- Registration
- Permissions
- Context sharing
- Tool exposure
- Runtime management

---

## RFC-015 — Tool System

Defines framework tools.

Examples include:

- Filesystem
- Git
- Browser
- Terminal
- Docker
- External APIs
- Custom tools

Also covers:

- Permissions
- Security
- Tool registration

---

# Phase 5 — Reliability

Focuses on correctness, stability, and safe execution.

---

## RFC-016 — Parallel Execution

Defines:

- Concurrent execution
- Worker isolation
- Synchronisation
- Scheduling interactions
- Resource sharing
- Safe concurrency

---

## RFC-017 — Workspace Ownership & Version Control

Defines:

- File ownership
- Workspace isolation
- Git integration
- Branch strategy
- Merge strategy
- Conflict prevention
- Locking

---

## RFC-018 — Security

Defines:

- Permission model
- Secrets
- Authentication
- Authorisation
- Sandboxing
- Trust boundaries

---

## RFC-019 — Failure Recovery

Defines:

- Error handling
- Rollbacks
- Checkpoints
- Recovery
- Retries
- Crash resilience

---

## RFC-020 — Observability

Defines:

- Logging
- Metrics
- Tracing
- Event history
- Performance monitoring
- Token monitoring
- Cost tracking

---

# Phase 6 — Human Experience

Defines how users interact with the framework.

---

## RFC-021 — Human Supervision

Defines:

- Human intervention
- Approval workflows
- Manual overrides
- Agent supervision
- Runtime interaction
- Review pipelines

---

## RFC-022 — Dashboard & Graph UI

Defines:

- Visual supervisor graph
- Runtime monitoring
- Agent management
- Memory explorer
- Event timeline
- Task dashboard

*(See `docs/rfc/RFC-022-Dashboard-Graph-UI.md` for official specification)*

---

## RFC-023 — Configuration

Defines:

- Global configuration
- Project configuration
- Runtime configuration
- Agent configuration
- Plugin configuration
- User preferences

---

## RFC-024 — SDK & APIs

Defines public interfaces.

Includes:

- Internal APIs
- SDKs
- Plugin APIs
- Extension APIs
- External integrations

---

# Future RFCs

The framework is intended to evolve over time.

Potential future RFCs include:

- Distributed execution
- Remote workers
- Cloud orchestration
- Agent reputation systems
- Learning agents
- Autonomous optimisation
- Simulation environments
- Marketplace for agents, skills, and plugins
- Visual workflow designer
- Enterprise administration
- Multi-project orchestration
- Federated memory systems

## RFC-025 — Dynamic Workflow Compilation
Defines the "Zero-Boilerplate Orchestrator" where natural language dynamically compiles to executed DAGs without custom `main.go` entrypoints.
*(See `docs/rfc/RFC-025-Dynamic-Workflow-Compilation.md` for official specification)*

## RFC-026 — Worker Fault Tolerance & Stdout Protocol
Defines the strict fault-tolerance constraints, mock fallback patterns, and line-by-line JSON payload extraction logic required for `Worker Protocol (v1)`.
*(See `docs/rfc/RFC-026-Worker-Stdout-Protocol.md` for official specification)*

## RFC-033 — Isolated Session Workspaces
Defines the separation of Global Project Scope from isolated Session Sandboxes, allowing `forge.exe` to execute multiple concurrent DAG workflows (prompts) without state clashing.
*(See `docs/rfc/RFC-033-Isolated-Session-Workspaces.md` for official specification)*

## RFC-034 — Complex Agentic Workflows via ReAct
Defines the upgrade of static Python workers into stateful ReAct agents capable of iterative tool execution and persistent memory tracking via the Go Event Bus.
*(See `docs/rfc/RFC-034-Complex-Agentic-Workflows.md` for official specification)*

## RFC-035 — Agent Evolution (The Meta-Scaffolder)
Defines the background telemetry loop where brittle `.yaml` and `worker.py` agent scripts are autonomously rewritten and hot-reloaded by a top-level meta-agent based on empirical failure rates.
*(See `docs/rfc/RFC-035-Agent-Evolution-Meta-Scaffolder.md` for official specification)*

## RFC-036 — Smart Predictive Rate Limiting
Defines the predictive token-bucket system in `router.go` to track RPM/TPM and gracefully pause requests before triggering provider 429 Quota Exceeded errors.

## RFC-037 — Dynamic Context Compression (RAG)
Defines the automatic indexing of workspaces into local vector DBs and context summarization loops for models with strict 8k window limits.

## RFC-038 — Human-in-the-Loop (HitL) Checkpoints
Defines the `hitl-agent` architecture for explicitly pausing DAG execution to request human approval for high-risk operations (e.g. deployments).

## RFC-039 — Advanced Engineering Skills
Defines the formal inclusion of `devops-infrastructure`, `vulnerability-assessment`, `ml-engineering`, and `modern-frontend-design` into the core Reticle skill ecosystem.

---

# Roadmap Philosophy

The roadmap is intentionally sequential.

Each phase builds upon concepts introduced by previous phases.

No RFC should duplicate concepts already defined elsewhere.

Each RFC should instead extend the architectural specification while remaining consistent with all previously accepted RFCs.

This roadmap is expected to evolve as the framework matures. New RFCs may be introduced, revised, superseded, or deprecated through the project's governance process.

Foundation Phase

✔ Repository Structure
✔ Roadmap
✔ Project State
✔ Metadata Standard (Draft)

↓
Document Standards

→ 001 DOCUMENT_WRITING_STANDARD
→ Template Suite
→ RFC Process

↓

Philosophy

→ RFC-000 Planning
→ RFC-000 Writing

↓

Architecture

RFC-001 Runtime
RFC-002 Events
RFC-003 Scheduler
...

1. Finish the event-driven runtime skeleton
   ✓ Event Bus
   ✓ Dispatcher
   ✓ Subscription Manager
   ✓ Artifact Store

2. Define the Worker Runtime Contract
   ✓ (RFC-027-Worker-Runtime-Contract)

3. Build a true Task Graph executor
   ✓ (GraphEngine implemented)

4. Introduce Shared Runtime Memory
   ✓ (RuntimeState and SessionState via memory event bus)

5. Write the first architectural RFCs
   ✓ (Initial architectural RFCs have been written)